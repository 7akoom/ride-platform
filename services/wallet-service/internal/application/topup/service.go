package topup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

const (
	// sweepWindow is how far back the sweep looks for pending top-ups. A
	// provider's payment session expires long before (ZainCash's in about
	// 15 minutes), so a top-up still pending after this is left for staff.
	sweepWindow = 48 * time.Hour
	// sweepAfter leaves alone a top-up the customer may still be paying.
	sweepAfter = time.Minute
	// unopenedAfter fails a top-up whose payment never opened at the
	// provider (it has no transaction id there).
	unopenedAfter = 15 * time.Minute
	// sweepBatch is how many pending top-ups one sweep looks at.
	sweepBatch = 100
)

type InitiateInput struct {
	OwnerType wallet.OwnerType
	OwnerID   string
	Amount    wallet.Money
	// Provider is empty for zaincash.
	Provider string
	// The app's language (ar, ku, en), for the provider's page.
	Language string
}

type Service interface {
	// Initiate creates a pending top-up and opens the provider's payment
	// session; it returns the record and the page to send the customer to.
	Initiate(ctx context.Context, input InitiateInput) (TopUp, string, error)
	// Get is the owner's top-up, as it stands. A pending one is checked with
	// the provider first, so the answer is the provider's latest.
	Get(ctx context.Context, ownerType wallet.OwnerType, ownerID, id string) (TopUp, error)
	// Options is what a top-up may be in this deployment.
	Options(ctx context.Context) (Options, error)
	// ProcessNotice verifies a provider's notification and settles its
	// top-up. Safe to call more than once for the same payment.
	ProcessNotice(ctx context.Context, provider, token string) error
	// SettlePending checks the top-ups still pending with their providers
	// and settles those that ended; it returns how many it settled.
	SettlePending(ctx context.Context) (int, error)
}

type service struct {
	repository    Repository
	config        ConfigReader
	providers     map[string]Provider
	walletService wallet.Service
	logger        *slog.Logger
	now           func() time.Time

	// Where the provider sends the customer's browser after they pay,
	// cancel or fail.
	successURL string
	failureURL string
}

// NewService takes the providers set up in this deployment; none is fine
// (top-ups through a provider are then off, and Options says so).
func NewService(
	repository Repository,
	config ConfigReader,
	providers map[string]Provider,
	walletService wallet.Service,
	successURL string,
	failureURL string,
	logger *slog.Logger,
) Service {
	if repository == nil {
		panic("topup repository is required")
	}

	if config == nil {
		panic("wallet config reader is required")
	}

	if walletService == nil {
		panic("wallet service is required")
	}

	if logger == nil {
		panic("logger is required")
	}

	known := make(map[string]Provider, len(providers))

	for name, provider := range providers {
		if provider != nil {
			known[name] = provider
		}
	}

	return &service{
		repository:    repository,
		config:        config,
		providers:     known,
		walletService: walletService,
		logger:        logger,
		now:           time.Now,
		successURL:    successURL,
		failureURL:    failureURL,
	}
}

func (s *service) Initiate(ctx context.Context, input InitiateInput) (TopUp, string, error) {
	ownerID := strings.TrimSpace(input.OwnerID)
	if !input.OwnerType.Valid() || !uuidPattern.MatchString(ownerID) {
		return TopUp{}, "", ErrOwnerRequired
	}

	if !input.Amount.IsPositive() || !input.Amount.Equal(input.Amount.Round(3)) {
		return TopUp{}, "", ErrInvalidAmount
	}

	name := strings.ToLower(strings.TrimSpace(input.Provider))
	if name == "" {
		name = ProviderZainCash
	}

	provider, ok := s.providers[name]
	if !ok {
		return TopUp{}, "", ErrUnknownProvider
	}

	config, err := s.config.GetActiveConfig(ctx)
	if err != nil {
		return TopUp{}, "", fmt.Errorf("read the wallet config: %w", err)
	}

	switch {
	case input.Amount.LessThan(config.TopUpMinAmount):
		return TopUp{}, "", ErrBelowMinimum
	case input.Amount.GreaterThan(config.TopUpMaxAmount):
		return TopUp{}, "", ErrAboveMaximum
	}

	created, err := s.repository.Create(ctx, TopUp{
		OwnerType:    input.OwnerType,
		OwnerID:      ownerID,
		Provider:     name,
		Amount:       input.Amount,
		CurrencyCode: config.CurrencyCode,
		Status:       StatusPending,
	})
	if err != nil {
		return TopUp{}, "", fmt.Errorf("create top-up record: %w", err)
	}

	result, err := provider.Start(ctx, StartInput{
		ReferenceID:  created.ExternalReferenceID,
		OrderID:      created.ID,
		OwnerType:    input.OwnerType,
		Amount:       input.Amount,
		CurrencyCode: config.CurrencyCode,
		Language:     input.Language,
		SuccessURL:   s.successURL,
		FailureURL:   s.failureURL,
	})
	if err != nil {
		if _, markErr := s.repository.MarkFailed(ctx, created.ExternalReferenceID, err.Error()); markErr != nil {
			return TopUp{}, "", fmt.Errorf("start the %s payment: %w (also failed to record it: %v)", name, err, markErr)
		}

		if errors.Is(err, ErrAmountNotSupported) {
			return TopUp{}, "", err
		}

		return TopUp{}, "", fmt.Errorf("start the %s payment: %w", name, err)
	}

	updated, err := s.repository.SetProviderTransactionID(ctx, created.ExternalReferenceID, result.ProviderTransactionID)
	if err != nil {
		return TopUp{}, "", fmt.Errorf("record the provider's transaction id: %w", err)
	}

	return updated, result.RedirectURL, nil
}

func (s *service) Get(ctx context.Context, ownerType wallet.OwnerType, ownerID, id string) (TopUp, error) {
	ownerID, id = strings.TrimSpace(ownerID), strings.TrimSpace(id)

	if !ownerType.Valid() || !uuidPattern.MatchString(ownerID) {
		return TopUp{}, ErrOwnerRequired
	}

	if !uuidPattern.MatchString(id) {
		return TopUp{}, ErrTopUpNotFound
	}

	found, err := s.repository.FindForOwner(ctx, ownerType, ownerID, id)
	if err != nil {
		return TopUp{}, err
	}

	if found.Status != StatusPending || found.ProviderTransactionID == "" {
		return found, nil
	}

	// The provider not answering is no reason to fail the app's question:
	// the top-up is still pending as far as anyone knows, and the sweep asks
	// again.
	checked, err := s.check(ctx, found)
	if err != nil {
		s.logger.Warn("a pending top-up could not be checked with its provider",
			"topup_id", found.ID, "provider", found.Provider, "error", err)

		return found, nil
	}

	return checked, nil
}

func (s *service) Options(ctx context.Context) (Options, error) {
	config, err := s.config.GetActiveConfig(ctx)
	if err != nil {
		return Options{}, fmt.Errorf("read the wallet config: %w", err)
	}

	names := make([]string, 0, len(s.providers))
	for name := range s.providers {
		names = append(names, name)
	}

	sort.Strings(names)

	return Options{
		MinAmount:    config.TopUpMinAmount,
		MaxAmount:    config.TopUpMaxAmount,
		CurrencyCode: config.CurrencyCode,
		Providers:    names,
	}, nil
}

func (s *service) ProcessNotice(ctx context.Context, name, token string) error {
	provider, ok := s.providers[name]
	if !ok {
		return ErrUnknownProvider
	}

	notice, err := provider.Verify(token)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidWebhookToken, err)
	}

	record, err := s.repository.FindByExternalReferenceID(ctx, notice.ReferenceID)
	if err != nil {
		return fmt.Errorf("find top-up by external reference id %q: %w", notice.ReferenceID, err)
	}

	// A notification for another provider's payment is not this one's.
	if record.Provider != name {
		return fmt.Errorf("%w: the payment belongs to another provider", ErrInvalidWebhookToken)
	}

	// Nor one naming another of the provider's transactions.
	if record.ProviderTransactionID != "" && notice.ProviderTransactionID != "" &&
		record.ProviderTransactionID != notice.ProviderTransactionID {
		return fmt.Errorf("%w: the notification names another transaction", ErrInvalidWebhookToken)
	}

	_, err = s.settle(ctx, record, notice)

	return err
}

func (s *service) SettlePending(ctx context.Context) (int, error) {
	now := s.now()

	pending, err := s.repository.ListPending(ctx, now.Add(-sweepWindow), now.Add(-sweepAfter), sweepBatch)
	if err != nil {
		return 0, fmt.Errorf("list pending top-ups: %w", err)
	}

	settled := 0

	for _, record := range pending {
		if ctx.Err() != nil {
			return settled, ctx.Err()
		}

		if record.ProviderTransactionID == "" {
			// The payment never opened at the provider: nobody can have paid it.
			if now.Sub(record.CreatedAt) < unopenedAfter {
				continue
			}

			if _, err := s.repository.MarkFailed(ctx, record.ExternalReferenceID, "the payment never opened at the provider"); err != nil && !errors.Is(err, ErrTopUpNotFound) {
				s.logger.Warn("an unopened top-up could not be closed", "topup_id", record.ID, "error", err)

				continue
			}

			settled++

			continue
		}

		checked, err := s.check(ctx, record)
		if err != nil {
			s.logger.Warn("a pending top-up could not be checked with its provider",
				"topup_id", record.ID, "provider", record.Provider, "error", err)

			continue
		}

		if checked.Status != StatusPending {
			settled++
		}
	}

	return settled, nil
}

// check asks the top-up's provider where its payment stands and settles it.
func (s *service) check(ctx context.Context, record TopUp) (TopUp, error) {
	provider, ok := s.providers[record.Provider]
	if !ok {
		// The provider is no longer set up here: nobody can ask it.
		return record, nil
	}

	notice, err := provider.Check(ctx, record.ProviderTransactionID)
	if err != nil {
		return record, err
	}

	if notice.ReferenceID != "" && notice.ReferenceID != record.ExternalReferenceID {
		return record, fmt.Errorf("the provider's answer names another payment (%s)", notice.ReferenceID)
	}

	return s.settle(ctx, record, notice)
}

// settle applies the provider's word to a pending top-up: a success credits
// the wallet once and closes it, a failure closes it, anything else leaves
// it pending. A top-up already closed stays as it is: a refund or a late
// failure never takes back money already credited (that is staff's call).
func (s *service) settle(ctx context.Context, record TopUp, notice Notice) (TopUp, error) {
	if record.Status != StatusPending {
		return record, nil
	}

	switch notice.Outcome {
	case OutcomeSucceeded:
		transactionID := record.ProviderTransactionID
		if transactionID == "" {
			transactionID = notice.ProviderTransactionID
		}

		if transactionID == "" {
			transactionID = "ref:" + record.ExternalReferenceID
		}

		// The key is the provider's own transaction id: it ties the credit
		// to the provider's record of the money having moved, so a second
		// word on the same payment (a retried webhook, the app asking, the
		// sweep) never credits twice.
		if _, _, err := s.walletService.TopUp(ctx, wallet.TopUpInput{
			OwnerType:      record.OwnerType,
			OwnerID:        record.OwnerID,
			Amount:         record.Amount,
			IdempotencyKey: record.Provider + ":" + transactionID,
			Description:    providerLabel(record.Provider) + " top-up",
		}); err != nil && !errors.Is(err, wallet.ErrDuplicateRequest) {
			return record, fmt.Errorf("credit wallet for top-up %s: %w", record.ID, err)
		}

		marked, err := s.repository.MarkSucceeded(ctx, record.ExternalReferenceID)
		if err != nil {
			return record, fmt.Errorf("mark top-up %s succeeded: %w", record.ID, err)
		}

		return marked, nil

	case OutcomeFailed:
		reason := strings.TrimSpace(notice.Detail)
		if reason == "" {
			reason = record.Provider + " reported the payment failed"
		}

		marked, err := s.repository.MarkFailed(ctx, record.ExternalReferenceID, reason)
		if errors.Is(err, ErrTopUpNotFound) {
			// Settled as succeeded meanwhile: that stands.
			return s.repository.FindByExternalReferenceID(ctx, record.ExternalReferenceID)
		}

		if err != nil {
			return record, fmt.Errorf("mark top-up %s failed: %w", record.ID, err)
		}

		return marked, nil
	}

	return record, nil
}

func providerLabel(name string) string {
	if name == ProviderZainCash {
		return "ZainCash"
	}

	return name
}

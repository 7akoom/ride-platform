package topup

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type InitiateInput struct {
	OwnerType wallet.OwnerType
	OwnerID   string
	Amount    wallet.Money
	// Provider is empty for zaincash.
	Provider string
}

type Service interface {
	// Initiate creates a pending top-up and opens the provider's payment
	// session; it returns the record and the page to send the customer to.
	Initiate(ctx context.Context, input InitiateInput) (TopUp, string, error)
	// Get is the owner's top-up, as it stands.
	Get(ctx context.Context, ownerType wallet.OwnerType, ownerID, id string) (TopUp, error)
	// ProcessNotice verifies a provider's notification and, on a final
	// success, credits the wallet through wallet.Service.TopUp. Safe to call
	// more than once for the same payment.
	ProcessNotice(ctx context.Context, provider, token string) error
}

type service struct {
	repository    Repository
	config        ConfigReader
	providers     map[string]Provider
	walletService wallet.Service

	// Where the provider sends the customer's browser after they pay,
	// cancel or fail.
	successURL string
	failureURL string
}

func NewService(
	repository Repository,
	config ConfigReader,
	providers map[string]Provider,
	walletService wallet.Service,
	successURL string,
	failureURL string,
) Service {
	if repository == nil {
		panic("topup repository is required")
	}

	if config == nil {
		panic("wallet config reader is required")
	}

	if providers[ProviderZainCash] == nil {
		panic("the zaincash provider is required")
	}

	if walletService == nil {
		panic("wallet service is required")
	}

	return &service{
		repository:    repository,
		config:        config,
		providers:     providers,
		walletService: walletService,
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

	return s.repository.FindForOwner(ctx, ownerType, ownerID, id)
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

	// Idempotent: a resolved top-up receiving another delivery (providers
	// retry) is a no-op, not an error.
	if record.Status != StatusPending {
		return nil
	}

	switch notice.Outcome {
	case OutcomeSucceeded:
		transactionID := record.ProviderTransactionID
		if transactionID == "" {
			transactionID = notice.ProviderTransactionID
		}

		// The key is the provider's own transaction id: it ties the credit
		// to the provider's record of the money having moved, so a
		// duplicate notification never credits twice.
		if _, _, err := s.walletService.TopUp(ctx, wallet.TopUpInput{
			OwnerType:      record.OwnerType,
			OwnerID:        record.OwnerID,
			Amount:         record.Amount,
			IdempotencyKey: name + ":" + transactionID,
			Description:    providerLabel(name) + " top-up",
		}); err != nil && !errors.Is(err, wallet.ErrDuplicateRequest) {
			return fmt.Errorf("credit wallet for top-up %s: %w", record.ID, err)
		}

		if _, err := s.repository.MarkSucceeded(ctx, record.ExternalReferenceID); err != nil {
			return fmt.Errorf("mark top-up %s succeeded: %w", record.ID, err)
		}

	case OutcomeFailed:
		if _, err := s.repository.MarkFailed(ctx, record.ExternalReferenceID, name+" reported the payment failed"); err != nil {
			return fmt.Errorf("mark top-up %s failed: %w", record.ID, err)
		}
	}

	return nil
}

func providerLabel(name string) string {
	if name == ProviderZainCash {
		return "ZainCash"
	}

	return name
}

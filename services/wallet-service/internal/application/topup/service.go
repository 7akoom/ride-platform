package topup

import (
	"context"
	"fmt"
	"strings"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

type InitiateInput struct {
	DriverID string
	Amount   wallet.Money
}

type Service interface {
	// Initiate creates a pending top-up record, opens a ZainCash
	// payment session, and returns the record plus the redirectUrl
	// the caller should send the driver to.
	Initiate(ctx context.Context, input InitiateInput) (TopUp, string, error)

	// ProcessWebhookToken verifies a ZainCash webhook (or redirect
	// callback) token and, on a final SUCCESS, credits the driver's
	// wallet via wallet.Service.TopUp. Safe to call more than once for
	// the same event — see the pending-status check below.
	ProcessWebhookToken(ctx context.Context, rawToken string) error
}

type service struct {
	repository    Repository
	zainCash      ZainCashClient
	walletService wallet.Service

	// Where ZainCash redirects the customer's browser after they
	// finish, cancel, or fail the payment. No app UI exists yet to
	// land on — these currently point at plain placeholder pages.
	// Revisit once the driver app exists.
	successURL string
	failureURL string
}

func NewService(
	repository Repository,
	zainCash ZainCashClient,
	walletService wallet.Service,
	successURL string,
	failureURL string,
) Service {
	if repository == nil {
		panic("topup repository is required")
	}

	if zainCash == nil {
		panic("zaincash client is required")
	}

	if walletService == nil {
		panic("wallet service is required")
	}

	return &service{
		repository:    repository,
		zainCash:      zainCash,
		walletService: walletService,
		successURL:    successURL,
		failureURL:    failureURL,
	}
}

func (s *service) Initiate(
	ctx context.Context,
	input InitiateInput,
) (TopUp, string, error) {
	driverID := strings.TrimSpace(input.DriverID)
	if driverID == "" {
		return TopUp{}, "", ErrDriverIDRequired
	}

	if !input.Amount.IsPositive() {
		return TopUp{}, "", ErrInvalidAmount
	}

	created, err := s.repository.Create(ctx, TopUp{
		DriverID: driverID,
		Amount:   input.Amount,
		Status:   StatusPending,
	})
	if err != nil {
		return TopUp{}, "", fmt.Errorf("create top-up record: %w", err)
	}

	result, err := s.zainCash.InitTransaction(ctx, InitTransactionInput{
		Language:            "ar",
		ExternalReferenceID: created.ExternalReferenceID,
		OrderID:             created.ID,
		ServiceType:         "driver_topup",
		AmountValue:         input.Amount.StringFixed(0),
		SuccessURL:          s.successURL,
		FailureURL:          s.failureURL,
	})
	if err != nil {
		if _, markErr := s.repository.MarkFailed(ctx, created.ExternalReferenceID, err.Error()); markErr != nil {
			return TopUp{}, "", fmt.Errorf(
				"init zaincash transaction: %w (also failed to record failure: %v)",
				err, markErr,
			)
		}

		return TopUp{}, "", fmt.Errorf("init zaincash transaction: %w", err)
	}

	updated, err := s.repository.SetZainCashTransactionID(ctx, created.ExternalReferenceID, result.TransactionID)
	if err != nil {
		return TopUp{}, "", fmt.Errorf("record zaincash transaction id: %w", err)
	}

	return updated, result.RedirectURL, nil
}

func (s *service) ProcessWebhookToken(ctx context.Context, rawToken string) error {
	event, err := s.zainCash.VerifyToken(rawToken)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidWebhookToken, err)
	}

	record, err := s.repository.FindByExternalReferenceID(ctx, event.MerchantReferenceID)
	if err != nil {
		return fmt.Errorf("find top-up by external reference id %q: %w", event.MerchantReferenceID, err)
	}

	// Idempotent: an already-resolved top-up receiving another webhook
	// delivery (ZainCash may retry) is a no-op, not an error.
	if record.Status != StatusPending {
		return nil
	}

	switch event.CurrentStatus {
	case zainCashStatusSuccess:
		// The idempotency key is ZainCash's own transaction id, not
		// our external_reference_id — it's what actually ties this
		// credit to ZainCash's record of the money having moved.
		if _, _, err := s.walletService.TopUp(ctx, wallet.TopUpInput{
			OwnerType:      wallet.OwnerDriver,
			OwnerID:        record.DriverID,
			Amount:         record.Amount,
			IdempotencyKey: "zaincash:" + record.ZainCashTransactionID,
			Description:    "ZainCash top-up",
		}); err != nil {
			return fmt.Errorf("credit wallet for top-up %s: %w", record.ID, err)
		}

		if _, err := s.repository.MarkSucceeded(ctx, record.ExternalReferenceID, record.ZainCashTransactionID); err != nil {
			return fmt.Errorf("mark top-up %s succeeded: %w", record.ID, err)
		}

	case zainCashStatusFailed:
		if _, err := s.repository.MarkFailed(ctx, record.ExternalReferenceID, "zaincash reported FAILED"); err != nil {
			return fmt.Errorf("mark top-up %s failed: %w", record.ID, err)
		}

	default:
		// Not a final status (PENDING, OTP_SENT, ...) — nothing to do
		// yet; a later webhook delivery will carry the final one.
	}

	return nil
}

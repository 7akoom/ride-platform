// Package topup orchestrates a driver funding their wallet via
// ZainCash: creating a payment session, then reconciling the result
// (webhook or manual inquiry) against wallet.Service.TopUp — the one
// place a balance actually changes. This package owns none of the
// ledger; it only tracks payment-attempt state and decides when to
// call into wallet.Service.
package topup

import (
	"time"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

type Status string

const (
	StatusPending   Status = "pending"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
)

func (s Status) Valid() bool {
	switch s {
	case StatusPending, StatusSucceeded, StatusFailed:
		return true
	default:
		return false
	}
}

type TopUp struct {
	ID                    string
	DriverID              string
	ExternalReferenceID   string
	ZainCashTransactionID string
	Amount                wallet.Money
	Status                Status
	FailureReason         string
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// InitTransactionInput/Result and WebhookEvent are this package's OWN
// view of a ZainCash payment session — deliberately not the
// infrastructure/zaincash package's types, so this domain package
// doesn't depend on that HTTP/JWT client directly (see
// infrastructure/zaincash's adapter, which implements ZainCashClient
// below by translating to/from these).
type InitTransactionInput struct {
	Language            string
	ExternalReferenceID  string
	OrderID              string
	ServiceType          string
	AmountValue          string
	CustomerPhone        string
	SuccessURL           string
	FailureURL           string
}

type InitTransactionResult struct {
	TransactionID string
	RedirectURL   string
}

// WebhookEvent is the decoded, signature-verified content of a
// ZainCash webhook or redirect-callback token.
type WebhookEvent struct {
	EventID             string
	EventType           string
	TransactionID       string
	MerchantReferenceID string
	OrderID             string
	CurrentStatus       string
	PreviousStatus      string
}

// zainCashStatusSuccess/Failed are the transaction status values (see
// ZainCash's docs) that this package treats as final. Every other
// status (PENDING, OTP_SENT, CUSTOMER_AUTHENTICATION_REQUIRED,
// EXPIRED, ...) is left alone: ProcessWebhook only acts on a
// definitive outcome.
const (
	zainCashStatusSuccess = "SUCCESS"
	zainCashStatusFailed  = "FAILED"
)

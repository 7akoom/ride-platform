// Package topup is a wallet funded through a payment provider: a payment
// session on the provider's own page, then the provider's notification of
// how it ended, reconciled against wallet.Service.TopUp, the one place a
// balance changes. This package owns none of the ledger; it tracks the
// payment attempt and decides when to credit.
package topup

import (
	"context"
	"time"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

type Status string

const (
	StatusPending   Status = "pending"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
)

// ProviderZainCash is the provider every deployment has, and the default.
const ProviderZainCash = "zaincash"

type TopUp struct {
	ID                    string
	OwnerType             wallet.OwnerType
	OwnerID               string
	Provider              string
	ExternalReferenceID   string
	ProviderTransactionID string
	Amount                wallet.Money
	CurrencyCode          string
	Status                Status
	FailureReason         string
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// StartInput is a payment session to open at the provider.
type StartInput struct {
	// ReferenceID is ours: the provider echoes it back in its notification.
	ReferenceID  string
	OrderID      string
	OwnerType    wallet.OwnerType
	Amount       wallet.Money
	CurrencyCode string
	SuccessURL   string
	FailureURL   string
}

// StartResult is the provider's session: its id, and the page the customer
// pays on.
type StartResult struct {
	ProviderTransactionID string
	RedirectURL           string
}

// Outcome is where the provider says a payment stands.
type Outcome string

const (
	OutcomePending   Outcome = "pending"
	OutcomeSucceeded Outcome = "succeeded"
	OutcomeFailed    Outcome = "failed"
)

// Notice is a provider's verified notification about a payment.
type Notice struct {
	ReferenceID           string
	ProviderTransactionID string
	Outcome               Outcome
}

// Provider is a payment provider with a hosted payment page: the customer
// pays on the provider's page, so a card number or a wallet PIN never
// reaches the platform, and no provider here may ask for one. A card
// processor is one more Provider.
type Provider interface {
	Start(ctx context.Context, input StartInput) (StartResult, error)
	// Verify checks a notification's signature and reads it; an error means
	// it is not the provider's.
	Verify(token string) (Notice, error)
}

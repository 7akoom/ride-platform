// Package topup is a wallet funded through a payment provider: a payment
// session on the provider's own page, then the provider's word on how it
// ended, reconciled against wallet.Service.TopUp, the one place a balance
// changes. This package owns none of the ledger; it tracks the payment
// attempt and decides when to credit.
//
// The provider's word comes three ways, and any of them settles the top-up
// once: its notification (webhook, or the token on the page it sends the
// customer back to), the app asking how the top-up stands, and a sweep of
// the top-ups still pending. The last two ask the provider itself (Check),
// so a lost notification never leaves a paid top-up uncredited.
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

// ProviderZainCash is the provider every deployment can have, and the
// default when a request names none.
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
	// The app's language (ar, ku, en), for the provider's page.
	Language   string
	SuccessURL string
	FailureURL string
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

// Notice is the provider's word on a payment: a verified notification, or
// its answer when asked.
type Notice struct {
	ReferenceID           string
	ProviderTransactionID string
	Outcome               Outcome
	// The provider's own status (and error message), kept as the reason of a
	// failed top-up; for staff, never shown to customers.
	Detail string
}

// Provider is a payment provider with a hosted payment page: the customer
// pays on the provider's page, so a card number or a wallet PIN never
// reaches the platform, and no provider here may ask for one. A card
// processor is one more Provider.
type Provider interface {
	Start(ctx context.Context, input StartInput) (StartResult, error)
	// Check asks the provider, server to server, where the payment with its
	// transaction id stands.
	Check(ctx context.Context, providerTransactionID string) (Notice, error)
	// Verify checks a notification's signature and reads it; an error means
	// it is not the provider's.
	Verify(token string) (Notice, error)
}

// Options is what a top-up may be in this deployment.
type Options struct {
	MinAmount    wallet.Money
	MaxAmount    wallet.Money
	CurrencyCode string
	// The providers set up, in a stable order; empty when none is, and then
	// a wallet takes money through vouchers only.
	Providers []string
}

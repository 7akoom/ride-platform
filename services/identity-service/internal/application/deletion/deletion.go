// Package deletion deletes a person's account the way the big ride apps do:
// they confirm with a fresh code, every session ends at once, and after a
// grace period the personal data is erased everywhere (identity-service
// itself, and the other services through the identity.deleted event) while
// trips, money movements and tickets stay without pointing at anyone. Signing
// in again during the grace period cancels it (the session store does that,
// in the same transaction as the new session).
package deletion

import (
	"context"
	"errors"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

var (
	// ErrBlocked: something stands in the way (see Standing.Blockers).
	ErrBlocked = errors.New("the account cannot be deleted yet")
	// ErrBalanceNotAccepted: a positive balance would be lost and the person
	// did not accept it.
	ErrBalanceNotAccepted = errors.New("deleting the account loses the wallet balance: accept it to go on")
	// ErrAlreadyPending: a deletion is already waiting for its grace period.
	ErrAlreadyPending = errors.New("the account is already being deleted")
	// ErrNotActive: a suspended or disabled account cannot ask.
	ErrNotActive = errors.New("the account is not active")
	// ErrDeliveryFailed: the code could not be sent.
	ErrDeliveryFailed = errors.New("the code could not be sent, try again")
	// ErrUnavailable: a service needed to check the account did not answer.
	ErrUnavailable = errors.New("the account could not be checked, try again")
)

// Blocker is something that has to be settled before the account can go.
type Blocker string

const (
	BlockerActiveTrip      Blocker = "active_trip"
	BlockerScheduledTrip   Blocker = "scheduled_trip"
	BlockerUnpaidFees      Blocker = "unpaid_fees"
	BlockerNegativeBalance Blocker = "negative_balance"
	BlockerOpenPayout      Blocker = "open_payout"
)

// Balance is money in one of the person's wallets.
type Balance struct {
	// rider or driver.
	OwnerType string
	Amount    string
	Currency  string
}

// Standing is what the other services say about the person now.
type Standing struct {
	// The person's profiles; empty when they have none.
	RiderID  string
	DriverID string

	Blockers []Blocker
	// Positive balances, lost with the account.
	Balances []Balance
}

// Status of a deletion.
type Status string

const (
	StatusNone      Status = "none"
	StatusPending   Status = "pending"
	StatusCancelled Status = "cancelled"
	StatusCompleted Status = "completed"
)

// Deletion is an identity's latest deletion.
type Deletion struct {
	IdentityID  string
	Status      Status
	RiderID     string
	DriverID    string
	RequestedAt time.Time
	PurgeAfter  time.Time
}

// Accounts asks the services that hold the person's trips and money.
// Implementations fail (wrapping ErrUnavailable) rather than guess.
type Accounts interface {
	Standing(ctx context.Context, identityID string) (Standing, error)
}

// Media erases every file of an identity.
type Media interface {
	DeleteOwnerMedia(ctx context.Context, identityID string) error
}

// ConfirmInput records a confirmed deletion.
type ConfirmInput struct {
	ChallengeID         string
	IdentityID          string
	VerifiedAt          time.Time
	RiderID             string
	DriverID            string
	PurgeAfter          time.Time
	BalanceLossAccepted bool
}

// CompleteInput erases an identity whose grace period is over.
type CompleteInput struct {
	IdentityID  string
	RiderID     string
	DriverID    string
	CompletedAt time.Time
}

// Store keeps deletions. Confirm, Cancel (in the session store) and Complete
// each happen in one transaction with their outbox event.
type Store interface {
	// Find returns the identity's latest deletion; found is false when it
	// never asked.
	Find(ctx context.Context, identityID string) (Deletion, bool, error)

	// CreateChallenge stores the code's challenge, cancelling the identity's
	// earlier unused deletion challenges.
	CreateChallenge(ctx context.Context, challenge auth.OTPChallenge) error

	// Confirm marks the challenge used (auth.ErrChallengeUsed if it already
	// was), records the pending deletion, revokes every session of the
	// identity and writes identity.deletion_requested.
	Confirm(ctx context.Context, input ConfirmInput) (Deletion, error)

	// Due returns pending deletions whose grace period is over and that are
	// not waiting for another attempt.
	Due(ctx context.Context, now time.Time, limit int) ([]Deletion, error)

	// Postpone leaves a due deletion for later, noting why.
	Postpone(ctx context.Context, identityID string, until time.Time, reason string) error

	// Complete erases the identity's sign-in methods, sessions, codes and PIN,
	// disables it, marks the deletion completed and writes identity.deleted.
	// It does nothing (found false) when the deletion is no longer pending
	// (the person signed in meanwhile).
	Complete(ctx context.Context, input CompleteInput) (bool, error)
}

// Clock is the time source.
type Clock interface {
	Now() time.Time
}

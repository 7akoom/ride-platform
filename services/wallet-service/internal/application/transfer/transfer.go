// Package transfer is riders sending money to each other: from one wallet
// to another registered rider's, found by the phone they sign in with,
// confirmed with the sender's wallet PIN (kept by identity-service).
package transfer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

var (
	ErrSenderRequired       = errors.New("the sending rider is required")
	ErrIdentityRequired     = errors.New("a transfer is sent by a person, with their PIN")
	ErrIdempotencyKeyNeeded = errors.New("idempotency_key is required (1-120 characters)")
	ErrInvalidAmount        = errors.New("amount must be a positive decimal with at most 3 decimal places")
	ErrNoteTooLong          = errors.New("the note must be at most 140 characters")
	ErrInvalidPhone         = errors.New("recipient_phone must be in international form, like +9647701234567")
	ErrRecipientNotFound    = errors.New("no rider signs in with this phone")
	ErrToSelf               = errors.New("a rider cannot send money to themselves")
	// ErrKeyReused: the idempotency key was used for another transfer.
	ErrKeyReused  = errors.New("this idempotency_key was already used for another transfer")
	ErrPINNotSet  = errors.New("set a wallet PIN first")
	ErrBelowMin   = errors.New("the amount is below the least a transfer may move")
	ErrAboveMax   = errors.New("the amount is above the most a transfer may move")
	ErrDailyLimit = errors.New("this would pass how much you may send in 24 hours")
	ErrDailyCount = errors.New("you have sent as many transfers as allowed in 24 hours")
)

// WrongPINError is a wrong PIN, with the attempts left before it locks.
type WrongPINError struct{ AttemptsLeft int }

func (e *WrongPINError) Error() string {
	return fmt.Sprintf("wrong PIN: %d attempts left before it locks", e.AttemptsLeft)
}

// LockedPINError is a PIN locked after too many wrong attempts.
type LockedPINError struct{ Until time.Time }

func (e *LockedPINError) Error() string {
	return "the PIN is locked after too many wrong attempts until " + e.Until.UTC().Format(time.RFC3339)
}

// Direction is a transfer seen from one side.
type Direction string

const (
	Sent     Direction = "sent"
	Received Direction = "received"
)

// Transfer is money sent from one rider's wallet to another's.
type Transfer struct {
	ID               string
	SenderRiderID    string
	RecipientRiderID string
	SenderPhone      string
	RecipientPhone   string
	CurrencyCode     string
	Amount           wallet.Money
	Note             string
	IdempotencyKey   string
	CreatedAt        time.Time
}

// Seen is the transfer from the given rider's side: which way it went and
// the other rider's phone.
func (t Transfer) Seen(riderID string) (Direction, string) {
	if t.RecipientRiderID == riderID && t.SenderRiderID != riderID {
		return Received, t.SenderPhone
	}

	return Sent, t.RecipientPhone
}

// Record is what the store moves: the transfer and the limits it must stay
// within (checked under the sender's wallet lock, so two sends at once cannot
// both slip past them).
type Record struct {
	Transfer    Transfer
	DailyAmount wallet.Money
	DailyCount  int
}

// PINCheck is identity-service's answer about a PIN.
type PINCheck string

const (
	PINOK     PINCheck = "ok"
	PINWrong  PINCheck = "wrong"
	PINLocked PINCheck = "locked"
	PINNotSet PINCheck = "not_set"
)

// PINResult is a checked PIN.
type PINResult struct {
	Check        PINCheck
	AttemptsLeft int
	LockedUntil  time.Time
}

// Store moves the money.
type Store interface {
	Config(ctx context.Context) (wallet.Config, error)
	// FindByKey returns the sender's transfer with that idempotency key.
	FindByKey(ctx context.Context, senderRiderID, key string) (Transfer, bool, error)
	// Send moves the amount from the sender's wallet to the recipient's, with
	// both ledger rows and the event, in one transaction. Errors:
	// wallet.ErrInsufficientFunds, wallet.ErrWalletBlocked, ErrDailyLimit,
	// ErrDailyCount, wallet.ErrDuplicateRequest (the key was taken meanwhile).
	Send(ctx context.Context, record Record) (Transfer, wallet.Wallet, error)
	// List is a rider's transfers, sent and received, newest first.
	List(ctx context.Context, riderID string, offset, limit int) ([]Transfer, error)
}

// Identity is identity-service: PINs and phones.
type Identity interface {
	VerifyPIN(ctx context.Context, identityID, pin string) (PINResult, error)
	// FindByPhone returns the active identity that signs in with the phone;
	// ErrInvalidPhone for one that is not E.164.
	FindByPhone(ctx context.Context, phone string) (string, bool, error)
	PhoneOf(ctx context.Context, identityID string) (string, error)
}

// Riders finds a person's rider profile.
type Riders interface {
	RiderID(ctx context.Context, identityID string) (string, error)
}

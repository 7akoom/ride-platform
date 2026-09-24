// Package walletpin keeps the PIN a person confirms money leaving their
// wallet with, and finds people by the phone they sign in with (for sending
// them money). The wallet itself lives in wallet-service, which asks here.
package walletpin

import (
	"context"
	"errors"
	"time"
)

var (
	ErrIdentityRequired = errors.New("identity is required")
	// ErrInvalidPIN: not 4 or 6 digits.
	ErrInvalidPIN = errors.New("the PIN must be 4 or 6 digits")
	// ErrWeakPIN: all the same digit, or a straight run.
	ErrWeakPIN = errors.New("the PIN is too easy to guess: not all the same digit and not a straight run like 1234")
	// ErrCurrentPINRequired: changing a PIN needs the current one, unless
	// the caller signed in moments ago.
	ErrCurrentPINRequired = errors.New("the current PIN is required (or sign in again to set a new one)")
	ErrWrongPIN           = errors.New("the current PIN is wrong")
	ErrPINLocked          = errors.New("the PIN is locked after too many wrong attempts")
	ErrInvalidPhoneNumber = errors.New("the phone number must be in international form, like +9647701234567")
)

// Policy on wrong PINs and on setting a new one without the old.
const (
	MaxFailedAttempts = 5
	BaseLockout       = 15 * time.Minute
	MaxLockout        = 24 * time.Hour
	// FreshSignInWindow is how long after signing in (with a one-time code)
	// a person may set a new PIN without the current one.
	FreshSignInWindow = 10 * time.Minute
)

// Record is a stored PIN and its wrong attempts.
type Record struct {
	IdentityID     string
	PINHash        string
	FailedAttempts int
	Lockouts       int
	LockedUntil    *time.Time
	SetAt          time.Time
}

// LockedAt reports whether the PIN is locked at now.
func (r Record) LockedAt(now time.Time) bool {
	return r.LockedUntil != nil && now.Before(*r.LockedUntil)
}

// Status is what a person sees about their PIN.
type Status struct {
	IsSet        bool
	SetAt        *time.Time
	LockedUntil  *time.Time
	AttemptsLeft int
}

// Check is the outcome of verifying a PIN.
type Check string

const (
	CheckOK     Check = "ok"
	CheckWrong  Check = "wrong"
	CheckLocked Check = "locked"
	CheckNotSet Check = "not_set"
)

// VerifyResult is a check, with the attempts left or the lock's end.
type VerifyResult struct {
	Check        Check
	AttemptsLeft int
	LockedUntil  *time.Time
}

// Change is what an Update callback decides: the record to store (when
// Save), and the error the caller gets once it is stored.
type Change struct {
	Record Record
	Save   bool
	Err    error
}

// Repository stores PINs.
type Repository interface {
	Find(ctx context.Context, identityID string) (Record, bool, error)
	// Update runs decide on the identity's PIN with its row locked (found is
	// false when there is none), stores what it says to, commits, and
	// returns the stored (or current) record and decide's error. Wrong
	// attempts are counted one at a time: two guesses never race.
	Update(ctx context.Context, identityID string, decide func(current Record, found bool) Change) (Record, error)
	// SessionStartedAt is when the identity's live session began (its
	// sign-in); false when the session is unknown, revoked or expired.
	SessionStartedAt(ctx context.Context, identityID, sessionID string) (time.Time, bool, error)
}

// Hasher makes and checks the slow hash of a PIN, bound to the identity.
type Hasher interface {
	Hash(identityID, pin string) (string, error)
	Compare(hash, identityID, pin string) (bool, error)
}

// Directory finds active identities by phone and the other way round.
type Directory interface {
	FindActiveByPhone(ctx context.Context, phoneNumber string) (string, bool, error)
	PhoneOf(ctx context.Context, identityID string) (string, error)
}

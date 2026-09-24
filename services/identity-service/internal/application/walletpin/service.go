package walletpin

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var (
	pinPattern   = regexp.MustCompile(`^([0-9]{4}|[0-9]{6})$`)
	phonePattern = regexp.MustCompile(`^\+[1-9][0-9]{1,14}$`)
)

// Service is what the PIN and directory endpoints call.
type Service struct {
	repository Repository
	hasher     Hasher
	directory  Directory
	now        func() time.Time
}

func NewService(repository Repository, hasher Hasher, directory Directory) *Service {
	if repository == nil {
		panic("wallet PIN repository is required")
	}

	if hasher == nil {
		panic("wallet PIN hasher is required")
	}

	if directory == nil {
		panic("identity directory is required")
	}

	return &Service{repository: repository, hasher: hasher, directory: directory, now: func() time.Time { return time.Now().UTC() }}
}

// Status says whether the identity has a PIN and whether it is locked.
func (s *Service) Status(ctx context.Context, identityID string) (Status, error) {
	identityID = strings.TrimSpace(identityID)
	if identityID == "" {
		return Status{}, ErrIdentityRequired
	}

	record, found, err := s.repository.Find(ctx, identityID)
	if err != nil {
		return Status{}, fmt.Errorf("read wallet PIN: %w", err)
	}

	return statusOf(record, found, s.now()), nil
}

// SetInput sets a PIN. SessionID is the caller's session: signed in moments
// ago, it may set a new PIN without the current one.
type SetInput struct {
	IdentityID string
	SessionID  string
	NewPIN     string
	CurrentPIN string
}

// Set sets or changes the identity's PIN. A wrong current PIN counts as a
// wrong attempt.
func (s *Service) Set(ctx context.Context, input SetInput) (Status, error) {
	identityID := strings.TrimSpace(input.IdentityID)
	if identityID == "" {
		return Status{}, ErrIdentityRequired
	}

	if err := CheckPIN(input.NewPIN); err != nil {
		return Status{}, err
	}

	hash, err := s.hasher.Hash(identityID, input.NewPIN)
	if err != nil {
		return Status{}, fmt.Errorf("hash wallet PIN: %w", err)
	}

	current := strings.TrimSpace(input.CurrentPIN)
	now := s.now()

	freshSignIn := false

	if current == "" {
		started, live, err := s.repository.SessionStartedAt(ctx, identityID, strings.TrimSpace(input.SessionID))
		if err != nil {
			return Status{}, fmt.Errorf("read the caller's session: %w", err)
		}

		freshSignIn = live && now.Sub(started) <= FreshSignInWindow
	}

	var decideErr error

	stored, err := s.repository.Update(ctx, identityID, func(record Record, found bool) Change {
		fresh := Record{IdentityID: identityID, PINHash: hash, SetAt: now}

		switch {
		case !found, freshSignIn:
			// The first PIN, or a forgotten one after signing in again: set
			// it, clearing wrong attempts and any lock.
			return Change{Record: fresh, Save: true}

		case current == "":
			return Change{Record: record, Err: ErrCurrentPINRequired}

		case record.LockedAt(now):
			return Change{Record: record, Err: ErrPINLocked}
		}

		matches, compareErr := s.hasher.Compare(record.PINHash, identityID, current)
		if compareErr != nil {
			return Change{Record: record, Err: fmt.Errorf("compare wallet PIN: %w", compareErr)}
		}

		if !matches {
			failed := recordFailure(record, now)
			decideErr = ErrWrongPIN

			if failed.LockedAt(now) {
				decideErr = ErrPINLocked
			}

			return Change{Record: failed, Save: true, Err: decideErr}
		}

		return Change{Record: fresh, Save: true}
	})
	if err != nil {
		return statusOf(stored, stored.PINHash != "", now), err
	}

	return statusOf(stored, true, now), nil
}

// Verify checks the identity's PIN, counting a wrong one.
func (s *Service) Verify(ctx context.Context, identityID, pin string) (VerifyResult, error) {
	identityID = strings.TrimSpace(identityID)
	if identityID == "" {
		return VerifyResult{}, ErrIdentityRequired
	}

	now := s.now()
	result := VerifyResult{}

	_, err := s.repository.Update(ctx, identityID, func(record Record, found bool) Change {
		switch {
		case !found:
			result = VerifyResult{Check: CheckNotSet}

			return Change{Record: record}

		case record.LockedAt(now):
			result = VerifyResult{Check: CheckLocked, LockedUntil: record.LockedUntil}

			return Change{Record: record}
		}

		// A PIN of the wrong shape is simply wrong: it is counted like any
		// other guess.
		matches := false

		if pinPattern.MatchString(pin) {
			ok, compareErr := s.hasher.Compare(record.PINHash, identityID, pin)
			if compareErr != nil {
				return Change{Record: record, Err: fmt.Errorf("compare wallet PIN: %w", compareErr)}
			}

			matches = ok
		}

		if matches {
			result = VerifyResult{Check: CheckOK, AttemptsLeft: MaxFailedAttempts}
			if record.FailedAttempts == 0 && record.Lockouts == 0 && record.LockedUntil == nil {
				return Change{Record: record}
			}

			record.FailedAttempts, record.Lockouts, record.LockedUntil = 0, 0, nil

			return Change{Record: record, Save: true}
		}

		failed := recordFailure(record, now)
		if failed.LockedAt(now) {
			result = VerifyResult{Check: CheckLocked, LockedUntil: failed.LockedUntil}
		} else {
			result = VerifyResult{Check: CheckWrong, AttemptsLeft: MaxFailedAttempts - failed.FailedAttempts}
		}

		return Change{Record: failed, Save: true}
	})
	if err != nil {
		return VerifyResult{}, err
	}

	return result, nil
}

// FindByPhone returns the active identity that signs in with the phone.
func (s *Service) FindByPhone(ctx context.Context, phoneNumber string) (string, bool, error) {
	phoneNumber = strings.TrimSpace(phoneNumber)
	if !phonePattern.MatchString(phoneNumber) {
		return "", false, ErrInvalidPhoneNumber
	}

	return s.directory.FindActiveByPhone(ctx, phoneNumber)
}

// PhoneOf returns the phone the identity signs in with (empty for none).
func (s *Service) PhoneOf(ctx context.Context, identityID string) (string, error) {
	identityID = strings.TrimSpace(identityID)
	if identityID == "" {
		return "", ErrIdentityRequired
	}

	return s.directory.PhoneOf(ctx, identityID)
}

// recordFailure counts a wrong PIN; at the limit the PIN locks, each lock
// twice as long as the last (at most MaxLockout), and the count restarts.
func recordFailure(record Record, now time.Time) Record {
	record.FailedAttempts++

	if record.FailedAttempts < MaxFailedAttempts {
		return record
	}

	lockout := BaseLockout
	for i := 0; i < record.Lockouts && lockout < MaxLockout; i++ {
		lockout *= 2
	}

	lockout = min(lockout, MaxLockout)
	until := now.Add(lockout)

	record.FailedAttempts = 0
	record.Lockouts++
	record.LockedUntil = &until

	return record
}

func statusOf(record Record, found bool, now time.Time) Status {
	if !found {
		return Status{AttemptsLeft: MaxFailedAttempts}
	}

	setAt := record.SetAt
	status := Status{IsSet: true, SetAt: &setAt, AttemptsLeft: MaxFailedAttempts - record.FailedAttempts}

	if record.LockedAt(now) {
		status.LockedUntil = record.LockedUntil
		status.AttemptsLeft = 0
	}

	return status
}

// CheckPIN accepts 4 or 6 digits that are not all the same and not a
// straight run up or down.
func CheckPIN(pin string) error {
	if !pinPattern.MatchString(pin) {
		return ErrInvalidPIN
	}

	same, up, down := true, true, true

	for i := 1; i < len(pin); i++ {
		step := int(pin[i]) - int(pin[i-1])
		same = same && step == 0
		up = up && step == 1
		down = down && step == -1
	}

	if same || up || down {
		return ErrWeakPIN
	}

	return nil
}

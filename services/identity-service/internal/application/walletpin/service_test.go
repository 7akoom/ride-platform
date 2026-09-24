package walletpin

import (
	"context"
	"errors"
	"testing"
	"time"
)

const identity = "11111111-1111-4111-8111-111111111111"

type fakeRepository struct {
	record  Record
	found   bool
	started time.Time
	live    bool
	saves   int
}

func (r *fakeRepository) Find(context.Context, string) (Record, bool, error) {
	return r.record, r.found, nil
}

func (r *fakeRepository) Update(_ context.Context, _ string, decide func(Record, bool) Change) (Record, error) {
	change := decide(r.record, r.found)
	if !change.Save {
		return r.record, change.Err
	}

	r.record, r.found = change.Record, true
	r.saves++

	return r.record, change.Err
}

func (r *fakeRepository) SessionStartedAt(context.Context, string, string) (time.Time, bool, error) {
	return r.started, r.live, nil
}

// fakeHasher "hashes" by prefixing: enough to tell PINs and identities apart.
type fakeHasher struct{}

func (fakeHasher) Hash(identityID, pin string) (string, error) { return identityID + "/" + pin, nil }

func (fakeHasher) Compare(hash, identityID, pin string) (bool, error) {
	return hash == identityID+"/"+pin, nil
}

type fakeDirectory struct{}

func (fakeDirectory) FindActiveByPhone(_ context.Context, phone string) (string, bool, error) {
	return identity, phone == "+9647701234567", nil
}

func (fakeDirectory) PhoneOf(context.Context, string) (string, error) { return "+9647701234567", nil }

var now = time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)

func newTestService() (*Service, *fakeRepository) {
	repo := &fakeRepository{}
	service := NewService(repo, fakeHasher{}, fakeDirectory{})
	service.now = func() time.Time { return now }

	return service, repo
}

func TestWhichPINsAreAccepted(t *testing.T) {
	for pin, want := range map[string]error{
		"2580":    nil,
		"915273":  nil,
		"1357":    nil,
		"123":     ErrInvalidPIN,
		"12345":   ErrInvalidPIN,
		"12a4":    ErrInvalidPIN,
		" 2580":   ErrInvalidPIN,
		"١٢٣٤":    ErrInvalidPIN,
		"0000":    ErrWeakPIN,
		"777777":  ErrWeakPIN,
		"1234":    ErrWeakPIN,
		"4321":    ErrWeakPIN,
		"456789":  ErrWeakPIN,
		"987654":  ErrWeakPIN,
		"1231":    nil,
		"0123456": ErrInvalidPIN,
	} {
		if got := CheckPIN(pin); !errors.Is(got, want) {
			t.Fatalf("%q: got %v, want %v", pin, got, want)
		}
	}
}

func TestSettingTheFirstPINAndChangingIt(t *testing.T) {
	service, repo := newTestService()

	status, err := service.Set(context.Background(), SetInput{IdentityID: identity, NewPIN: "2580"})
	if err != nil || !status.IsSet || status.AttemptsLeft != MaxFailedAttempts {
		t.Fatalf("first: %+v %v", status, err)
	}

	if _, err := service.Set(context.Background(), SetInput{IdentityID: identity, NewPIN: "1357"}); !errors.Is(err, ErrCurrentPINRequired) {
		t.Fatalf("without the current one: %v", err)
	}

	if _, err := service.Set(context.Background(), SetInput{IdentityID: identity, NewPIN: "1357", CurrentPIN: "9999"}); !errors.Is(err, ErrWrongPIN) {
		t.Fatalf("a wrong current one: %v", err)
	}

	if repo.record.FailedAttempts != 1 {
		t.Fatalf("the wrong current PIN must count: %+v", repo.record)
	}

	status, err = service.Set(context.Background(), SetInput{IdentityID: identity, NewPIN: "1357", CurrentPIN: "2580"})
	if err != nil || status.AttemptsLeft != MaxFailedAttempts || repo.record.PINHash != identity+"/1357" {
		t.Fatalf("changed: %+v %v %+v", status, err, repo.record)
	}
}

func TestAForgottenPINAfterSigningInAgain(t *testing.T) {
	service, repo := newTestService()
	repo.record = Record{IdentityID: identity, PINHash: identity + "/2580", SetAt: now.Add(-time.Hour)}
	repo.found = true

	// Signed in 20 minutes ago: too long.
	repo.started, repo.live = now.Add(-20*time.Minute), true
	if _, err := service.Set(context.Background(), SetInput{IdentityID: identity, SessionID: "s", NewPIN: "1357"}); !errors.Is(err, ErrCurrentPINRequired) {
		t.Fatalf("an old sign-in: %v", err)
	}

	// Locked, then signed in again moments ago: a new PIN, and no lock.
	until := now.Add(time.Hour)
	repo.record.LockedUntil, repo.record.Lockouts = &until, 2
	repo.started = now.Add(-2 * time.Minute)

	status, err := service.Set(context.Background(), SetInput{IdentityID: identity, SessionID: "s", NewPIN: "1357"})
	if err != nil || status.LockedUntil != nil || repo.record.Lockouts != 0 || repo.record.PINHash != identity+"/1357" {
		t.Fatalf("reset: %+v %v %+v", status, err, repo.record)
	}

	// A revoked session does not count.
	repo.live = false
	if _, err := service.Set(context.Background(), SetInput{IdentityID: identity, SessionID: "s", NewPIN: "2468"}); !errors.Is(err, ErrCurrentPINRequired) {
		t.Fatalf("a dead session: %v", err)
	}
}

func TestWrongPINsLockItLongerEachTime(t *testing.T) {
	service, repo := newTestService()
	repo.record = Record{IdentityID: identity, PINHash: identity + "/2580", SetAt: now}
	repo.found = true

	for i := 1; i < MaxFailedAttempts; i++ {
		result, err := service.Verify(context.Background(), identity, "1111")
		if err != nil || result.Check != CheckWrong || result.AttemptsLeft != MaxFailedAttempts-i {
			t.Fatalf("attempt %d: %+v %v", i, result, err)
		}
	}

	result, _ := service.Verify(context.Background(), identity, "1111")
	if result.Check != CheckLocked || !result.LockedUntil.Equal(now.Add(BaseLockout)) {
		t.Fatalf("locked: %+v", result)
	}

	// Even the right PIN is refused while locked, and nothing is counted.
	saves := repo.saves
	if result, _ := service.Verify(context.Background(), identity, "2580"); result.Check != CheckLocked || repo.saves != saves {
		t.Fatalf("right PIN while locked: %+v", result)
	}

	// The lock ends; five more wrong ones lock it twice as long.
	service.now = func() time.Time { return now.Add(time.Hour) }

	for i := 0; i < MaxFailedAttempts; i++ {
		result, _ = service.Verify(context.Background(), identity, "1111")
	}

	if result.Check != CheckLocked || !result.LockedUntil.Equal(now.Add(time.Hour).Add(2*BaseLockout)) {
		t.Fatalf("second lock: %+v", result)
	}

	// Much later the right PIN clears everything.
	service.now = func() time.Time { return now.Add(48 * time.Hour) }

	result, err := service.Verify(context.Background(), identity, "2580")
	if err != nil || result.Check != CheckOK || repo.record.Lockouts != 0 || repo.record.FailedAttempts != 0 || repo.record.LockedUntil != nil {
		t.Fatalf("cleared: %+v %v %+v", result, err, repo.record)
	}
}

func TestTheLockNeverExceedsADay(t *testing.T) {
	record := Record{Lockouts: 30, FailedAttempts: MaxFailedAttempts - 1}

	locked := recordFailure(record, now)
	if !locked.LockedUntil.Equal(now.Add(MaxLockout)) {
		t.Fatalf("until %v", locked.LockedUntil)
	}
}

func TestVerifyingWithoutAPINOrWithAMalformedOne(t *testing.T) {
	service, repo := newTestService()

	if result, err := service.Verify(context.Background(), identity, "2580"); err != nil || result.Check != CheckNotSet {
		t.Fatalf("no PIN: %+v %v", result, err)
	}

	repo.record = Record{IdentityID: identity, PINHash: identity + "/2580", SetAt: now}
	repo.found = true

	if result, _ := service.Verify(context.Background(), identity, "25"); result.Check != CheckWrong || repo.record.FailedAttempts != 1 {
		t.Fatalf("malformed: %+v %+v", result, repo.record)
	}
}

func TestFindingPeopleByPhone(t *testing.T) {
	service, _ := newTestService()

	if id, found, err := service.FindByPhone(context.Background(), " +9647701234567 "); err != nil || !found || id != identity {
		t.Fatalf("found %v %v %v", id, found, err)
	}

	for _, bad := range []string{"07701234567", "+0123", "+964 770 123 4567", ""} {
		if _, _, err := service.FindByPhone(context.Background(), bad); !errors.Is(err, ErrInvalidPhoneNumber) {
			t.Fatalf("%q: %v", bad, err)
		}
	}
}

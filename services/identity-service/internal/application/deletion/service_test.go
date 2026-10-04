package deletion

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

const (
	identityID = "11111111-1111-4111-8111-111111111111"
	riderID    = "22222222-2222-4222-8222-222222222222"
	driverID   = "33333333-3333-4333-8333-333333333333"
)

var testNow = time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

type fakeStore struct {
	deletion   *Deletion
	challenges []auth.OTPChallenge
	confirmed  []ConfirmInput
	postponed  []string
	completed  []CompleteInput
	due        []Deletion
}

func (s *fakeStore) Find(context.Context, string) (Deletion, bool, error) {
	if s.deletion == nil {
		return Deletion{}, false, nil
	}

	return *s.deletion, true, nil
}

func (s *fakeStore) CreateChallenge(_ context.Context, c auth.OTPChallenge) error {
	s.challenges = append(s.challenges, c)

	return nil
}

func (s *fakeStore) Confirm(_ context.Context, in ConfirmInput) (Deletion, error) {
	s.confirmed = append(s.confirmed, in)
	d := Deletion{IdentityID: in.IdentityID, Status: StatusPending, RequestedAt: in.VerifiedAt, PurgeAfter: in.PurgeAfter}
	s.deletion = &d

	return d, nil
}

func (s *fakeStore) Due(context.Context, time.Time, int) ([]Deletion, error) { return s.due, nil }

func (s *fakeStore) Postpone(_ context.Context, id string, _ time.Time, reason string) error {
	s.postponed = append(s.postponed, reason)

	return nil
}

func (s *fakeStore) Complete(_ context.Context, in CompleteInput) (bool, error) {
	s.completed = append(s.completed, in)

	return true, nil
}

type fakeIdentities struct{ details auth.IdentityDetails }

func (f fakeIdentities) FindByID(context.Context, string) (auth.IdentityDetails, bool, error) {
	return f.details, true, nil
}

type fakeAccounts struct {
	standing Standing
	err      error
}

func (f *fakeAccounts) Standing(context.Context, string) (Standing, error) { return f.standing, f.err }

type fakeMedia struct {
	err     error
	deleted []string
}

func (f *fakeMedia) DeleteOwnerMedia(_ context.Context, id string) error {
	f.deleted = append(f.deleted, id)

	return f.err
}

type fixedGenerator struct{}

func (fixedGenerator) Generate() (string, error) { return "123456", nil }

type plainHasher struct{}

func (plainHasher) Hash(challengeID, code string) (string, error) {
	return challengeID + ":" + code, nil
}

func (plainHasher) Compare(hash, challengeID, code string) (bool, error) {
	return hash == challengeID+":"+code, nil
}

type recordingDelivery struct{ sent []auth.OTPDeliveryInput }

func (d *recordingDelivery) Send(_ context.Context, in auth.OTPDeliveryInput) error {
	d.sent = append(d.sent, in)

	return nil
}

type allowAll struct{}

func (allowAll) Allow(context.Context, auth.OTPRequestScope, time.Time, auth.OTPRequestRateLimitPolicy) error {
	return nil
}

type fixedIDs struct{}

func (fixedIDs) Generate() (string, error) { return "challenge-1", nil }

// memoryChallenges finds the challenges the fake store created.
type memoryChallenges struct {
	store  *fakeStore
	failed int
}

func (m *memoryChallenges) Create(context.Context, auth.OTPChallenge) error { return nil }

func (m *memoryChallenges) FindByID(_ context.Context, id string) (auth.OTPChallenge, error) {
	for _, c := range m.store.challenges {
		if c.ID == id {
			c.MaxAttempts = 5
			c.FailedAttempts = int16(m.failed)

			return c, nil
		}
	}

	return auth.OTPChallenge{}, auth.ErrChallengeNotFound
}

func (m *memoryChallenges) RecordFailedAttempt(context.Context, string, time.Time) error {
	m.failed++

	return nil
}

func (m *memoryChallenges) MarkVerified(context.Context, string, time.Time) error { return nil }

func (m *memoryChallenges) Cancel(context.Context, string, time.Time) error { return nil }

type fixture struct {
	service    *Service
	store      *fakeStore
	accounts   *fakeAccounts
	media      *fakeMedia
	delivery   *recordingDelivery
	challenges *memoryChallenges
	clock      *fakeClock
}

func newFixture(t *testing.T) fixture {
	t.Helper()

	f := fixture{
		store:    &fakeStore{},
		accounts: &fakeAccounts{standing: Standing{RiderID: riderID}},
		media:    &fakeMedia{},
		delivery: &recordingDelivery{},
		clock:    &fakeClock{now: testNow},
	}
	f.challenges = &memoryChallenges{store: f.store}

	phone, err := auth.NewIdentifier(auth.IdentifierTypePhone, "+9647501234567")
	if err != nil {
		t.Fatal(err)
	}

	identities := fakeIdentities{details: auth.IdentityDetails{
		ID: identityID, Status: auth.IdentityStatusActive,
		Identifiers: []auth.IdentityDetailsIdentifier{{Identifier: phone}},
	}}

	f.service = NewService(f.store, identities, f.accounts, f.media, OTP{
		Generator: fixedGenerator{}, Hasher: plainHasher{}, Delivery: f.delivery, RateLimiter: allowAll{},
		ChallengeIDs: fixedIDs{}, Challenges: f.challenges, TTL: 5 * time.Minute,
	}, Settings{GracePeriod: 30 * 24 * time.Hour, RetryAfter: time.Hour}, f.clock,
		slog.New(slog.NewTextHandler(io.Discard, nil)))

	return f
}

func (f fixture) requestCode(t *testing.T) string {
	t.Helper()

	result, err := f.service.RequestOTP(context.Background(), RequestOTPInput{IdentityID: identityID, Channel: auth.OTPDeliveryChannelAuto})
	if err != nil {
		t.Fatalf("request code: %v", err)
	}

	return result.ChallengeID
}

func TestRequestOTPIsRefusedWhileSomethingStandsInTheWay(t *testing.T) {
	f := newFixture(t)
	f.accounts.standing.Blockers = []Blocker{BlockerActiveTrip, BlockerUnpaidFees}

	_, err := f.service.RequestOTP(context.Background(), RequestOTPInput{IdentityID: identityID})

	var blocked *BlockedError
	if !errors.Is(err, ErrBlocked) || !errors.As(err, &blocked) || len(blocked.Blockers) != 2 {
		t.Fatalf("got %v", err)
	}

	if len(f.store.challenges) != 0 || len(f.delivery.sent) != 0 {
		t.Fatal("a code was sent anyway")
	}
}

func TestRequestOTPSendsTheCodeToTheAccountsPhone(t *testing.T) {
	f := newFixture(t)

	id := f.requestCode(t)

	if len(f.delivery.sent) != 1 || f.delivery.sent[0].Purpose != auth.OTPPurposeDeleteAccount ||
		f.delivery.sent[0].Identifier.Value != "+9647501234567" || id != "challenge-1" {
		t.Fatalf("sent %+v", f.delivery.sent)
	}

	c := f.store.challenges[0]
	if c.TargetIdentityID == nil || *c.TargetIdentityID != identityID || !c.ExpiresAt.Equal(testNow.Add(5*time.Minute)) {
		t.Fatalf("challenge %+v", c)
	}

	if _, err := f.service.RequestOTP(context.Background(), RequestOTPInput{IdentityID: identityID, Channel: auth.OTPDeliveryChannelEmail}); !errors.Is(err, auth.ErrOTPDeliveryChannelUnavailable) {
		t.Fatalf("email without an email address: %v", err)
	}
}

func TestConfirmChecksTheCodeAndTheBalance(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := f.requestCode(t)

	if _, err := f.service.Confirm(ctx, ConfirmRequest{IdentityID: identityID, ChallengeID: id, Code: "000000"}); !errors.Is(err, auth.ErrInvalidOTP) {
		t.Fatalf("wrong code: %v", err)
	}

	if f.challenges.failed != 1 {
		t.Fatal("the wrong code was not counted")
	}

	if _, err := f.service.Confirm(ctx, ConfirmRequest{IdentityID: "44444444-4444-4444-8444-444444444444", ChallengeID: id, Code: "123456"}); !errors.Is(err, auth.ErrOTPChallengeTargetMismatch) {
		t.Fatalf("someone else's challenge: %v", err)
	}

	f.accounts.standing.Balances = []Balance{{OwnerType: "rider", Amount: "2500", Currency: "IQD"}}

	if _, err := f.service.Confirm(ctx, ConfirmRequest{IdentityID: identityID, ChallengeID: id, Code: "123456"}); !errors.Is(err, ErrBalanceNotAccepted) {
		t.Fatalf("balance not accepted: %v", err)
	}

	started, err := f.service.Confirm(ctx, ConfirmRequest{IdentityID: identityID, ChallengeID: id, Code: "123456", AcceptBalanceLoss: true})
	if err != nil {
		t.Fatal(err)
	}

	in := f.store.confirmed[0]
	if started.Status != StatusPending || !in.PurgeAfter.Equal(testNow.Add(30*24*time.Hour)) || !in.BalanceLossAccepted || in.RiderID != riderID {
		t.Fatalf("confirmed %+v / %+v", started, in)
	}

	if _, err := f.service.RequestOTP(ctx, RequestOTPInput{IdentityID: identityID}); !errors.Is(err, ErrAlreadyPending) {
		t.Fatalf("asking again: %v", err)
	}
}

func TestConfirmIsRefusedWhenATripStartedMeanwhile(t *testing.T) {
	f := newFixture(t)
	id := f.requestCode(t)
	f.accounts.standing.Blockers = []Blocker{BlockerActiveTrip}

	if _, err := f.service.Confirm(context.Background(), ConfirmRequest{IdentityID: identityID, ChallengeID: id, Code: "123456"}); !errors.Is(err, ErrBlocked) {
		t.Fatalf("got %v", err)
	}

	if len(f.store.confirmed) != 0 {
		t.Fatal("the deletion started anyway")
	}
}

func TestEraseDue(t *testing.T) {
	due := Deletion{IdentityID: identityID, Status: StatusPending, RiderID: riderID}

	t.Run("erases files first, then the account", func(t *testing.T) {
		f := newFixture(t)
		f.store.due = []Deletion{due}
		f.accounts.standing = Standing{RiderID: riderID, DriverID: driverID}

		if n := f.service.EraseDue(context.Background()); n != 1 {
			t.Fatalf("erased %d", n)
		}

		if len(f.media.deleted) != 1 || len(f.store.completed) != 1 {
			t.Fatalf("media %v, completed %v", f.media.deleted, f.store.completed)
		}

		if c := f.store.completed[0]; c.RiderID != riderID || c.DriverID != driverID {
			t.Fatalf("completed %+v", c)
		}
	})

	t.Run("postpones when something stands in the way", func(t *testing.T) {
		f := newFixture(t)
		f.store.due = []Deletion{due}
		f.accounts.standing.Blockers = []Blocker{BlockerActiveTrip}

		f.service.EraseDue(context.Background())

		if len(f.store.completed) != 0 || len(f.media.deleted) != 0 || len(f.store.postponed) != 1 ||
			!strings.Contains(f.store.postponed[0], "active_trip") {
			t.Fatalf("postponed %v, completed %v", f.store.postponed, f.store.completed)
		}
	})

	t.Run("postpones when the files cannot be erased or a service does not answer", func(t *testing.T) {
		f := newFixture(t)
		f.store.due = []Deletion{due}
		f.media.err = errors.New("media down")

		f.service.EraseDue(context.Background())

		f.media.err = nil
		f.accounts.err = ErrUnavailable
		f.service.EraseDue(context.Background())

		if len(f.store.completed) != 0 || len(f.store.postponed) != 2 {
			t.Fatalf("postponed %v, completed %v", f.store.postponed, f.store.completed)
		}
	})
}

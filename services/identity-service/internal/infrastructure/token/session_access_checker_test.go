package token

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

type sessionCheckerRevocationStore struct {
	revoked bool

	isRevokedErr error
	markErr      error

	isRevokedCalls int
	markCalls      int

	markedSessionID string
	markedTTL       time.Duration
}

func (s *sessionCheckerRevocationStore) IsRevoked(
	ctx context.Context,
	sessionID string,
) (bool, error) {
	s.isRevokedCalls++

	if s.isRevokedErr != nil {
		return false, s.isRevokedErr
	}

	return s.revoked, nil
}

func (s *sessionCheckerRevocationStore) MarkRevoked(
	ctx context.Context,
	sessionID string,
	ttl time.Duration,
) error {
	s.markCalls++
	s.markedSessionID = sessionID
	s.markedTTL = ttl

	return s.markErr
}

type sessionCheckerStateStore struct {
	state auth.SessionAccessState
	found bool
	err   error

	calls int
}

func (s *sessionCheckerStateStore) FindSessionAccessState(
	ctx context.Context,
	sessionID string,
) (auth.SessionAccessState, bool, error) {
	s.calls++

	if s.err != nil {
		return auth.SessionAccessState{}, false, s.err
	}

	return s.state, s.found, nil
}

type sessionCheckerClock struct {
	now time.Time
}

func (c *sessionCheckerClock) Now() time.Time {
	return c.now
}

func TestSessionAccessCheckerReturnsRevokedFromValkeyWithoutPostgres(
	t *testing.T,
) {
	revocationStore := &sessionCheckerRevocationStore{
		revoked: true,
	}

	stateStore := &sessionCheckerStateStore{}

	checker, err := NewSessionAccessChecker(
		revocationStore,
		stateStore,
		&sessionCheckerClock{
			now: time.Now().UTC(),
		},
	)
	if err != nil {
		t.Fatalf(
			"NewSessionAccessChecker() returned an error: %v",
			err,
		)
	}

	revoked, err := checker.IsRevoked(
		context.Background(),
		"session-123",
	)
	if err != nil {
		t.Fatalf(
			"IsRevoked() returned an error: %v",
			err,
		)
	}

	if !revoked {
		t.Fatal(
			"session was expected to be revoked",
		)
	}

	if stateStore.calls != 0 {
		t.Fatalf(
			"PostgreSQL state store calls = %d, expected 0",
			stateStore.calls,
		)
	}
}

func TestSessionAccessCheckerAllowsActivePostgresSession(
	t *testing.T,
) {
	now := time.Date(
		2026,
		time.August,
		11,
		16,
		0,
		0,
		0,
		time.UTC,
	)

	revocationStore :=
		&sessionCheckerRevocationStore{}

	stateStore := &sessionCheckerStateStore{
		found: true,
		state: auth.SessionAccessState{
			SessionExpiresAt: now.Add(
				time.Hour,
			),
			Revoked: false,
		},
	}

	checker, err := NewSessionAccessChecker(
		revocationStore,
		stateStore,
		&sessionCheckerClock{
			now: now,
		},
	)
	if err != nil {
		t.Fatalf(
			"NewSessionAccessChecker() returned an error: %v",
			err,
		)
	}

	revoked, err := checker.IsRevoked(
		context.Background(),
		"session-123",
	)
	if err != nil {
		t.Fatalf(
			"IsRevoked() returned an error: %v",
			err,
		)
	}

	if revoked {
		t.Fatal(
			"active session was unexpectedly reported as revoked",
		)
	}

	if stateStore.calls != 1 {
		t.Fatalf(
			"PostgreSQL state store calls = %d, expected 1",
			stateStore.calls,
		)
	}
}

func TestSessionAccessCheckerRejectsMissingSession(
	t *testing.T,
) {
	revocationStore :=
		&sessionCheckerRevocationStore{}

	stateStore := &sessionCheckerStateStore{
		found: false,
	}

	checker, err := NewSessionAccessChecker(
		revocationStore,
		stateStore,
		&sessionCheckerClock{
			now: time.Now().UTC(),
		},
	)
	if err != nil {
		t.Fatalf(
			"NewSessionAccessChecker() returned an error: %v",
			err,
		)
	}

	revoked, err := checker.IsRevoked(
		context.Background(),
		"session-missing",
	)
	if err != nil {
		t.Fatalf(
			"IsRevoked() returned an error: %v",
			err,
		)
	}

	if !revoked {
		t.Fatal(
			"missing session was expected to be rejected",
		)
	}
}

func TestSessionAccessCheckerRebuildsMarkerForRevokedPostgresSession(
	t *testing.T,
) {
	now := time.Date(
		2026,
		time.August,
		11,
		16,
		0,
		0,
		0,
		time.UTC,
	)

	sessionExpiresAt := now.Add(
		2 * time.Hour,
	)

	revocationStore :=
		&sessionCheckerRevocationStore{}

	stateStore := &sessionCheckerStateStore{
		found: true,
		state: auth.SessionAccessState{
			SessionExpiresAt: sessionExpiresAt,
			Revoked:          true,
		},
	}

	checker, err := NewSessionAccessChecker(
		revocationStore,
		stateStore,
		&sessionCheckerClock{
			now: now,
		},
	)
	if err != nil {
		t.Fatalf(
			"NewSessionAccessChecker() returned an error: %v",
			err,
		)
	}

	revoked, err := checker.IsRevoked(
		context.Background(),
		"session-123",
	)
	if err != nil {
		t.Fatalf(
			"IsRevoked() returned an error: %v",
			err,
		)
	}

	if !revoked {
		t.Fatal(
			"revoked PostgreSQL session was expected to be rejected",
		)
	}

	if revocationStore.markCalls != 1 {
		t.Fatalf(
			"MarkRevoked() calls = %d, expected 1",
			revocationStore.markCalls,
		)
	}

	if revocationStore.markedSessionID !=
		"session-123" {
		t.Fatalf(
			"marked session ID = %q, expected %q",
			revocationStore.markedSessionID,
			"session-123",
		)
	}

	expectedTTL := 2 * time.Hour

	if revocationStore.markedTTL != expectedTTL {
		t.Fatalf(
			"marked TTL = %v, expected %v",
			revocationStore.markedTTL,
			expectedTTL,
		)
	}
}

func TestSessionAccessCheckerRejectsExpiredSession(
	t *testing.T,
) {
	now := time.Date(
		2026,
		time.August,
		11,
		16,
		0,
		0,
		0,
		time.UTC,
	)

	revocationStore :=
		&sessionCheckerRevocationStore{}

	stateStore := &sessionCheckerStateStore{
		found: true,
		state: auth.SessionAccessState{
			SessionExpiresAt: now.Add(
				-time.Minute,
			),
			Revoked: false,
		},
	}

	checker, err := NewSessionAccessChecker(
		revocationStore,
		stateStore,
		&sessionCheckerClock{
			now: now,
		},
	)
	if err != nil {
		t.Fatalf(
			"NewSessionAccessChecker() returned an error: %v",
			err,
		)
	}

	revoked, err := checker.IsRevoked(
		context.Background(),
		"session-123",
	)
	if err != nil {
		t.Fatalf(
			"IsRevoked() returned an error: %v",
			err,
		)
	}

	if !revoked {
		t.Fatal(
			"expired session was expected to be rejected",
		)
	}
}

func TestSessionAccessCheckerFailsClosedWhenValkeyCheckFails(
	t *testing.T,
) {
	expectedErr := errors.New(
		"Valkey unavailable",
	)

	revocationStore :=
		&sessionCheckerRevocationStore{
			isRevokedErr: expectedErr,
		}

	stateStore := &sessionCheckerStateStore{}

	checker, err := NewSessionAccessChecker(
		revocationStore,
		stateStore,
		&sessionCheckerClock{
			now: time.Now().UTC(),
		},
	)
	if err != nil {
		t.Fatalf(
			"NewSessionAccessChecker() returned an error: %v",
			err,
		)
	}

	_, err = checker.IsRevoked(
		context.Background(),
		"session-123",
	)
	if err == nil {
		t.Fatal(
			"IsRevoked() expected an error",
		)
	}

	if stateStore.calls != 0 {
		t.Fatalf(
			"PostgreSQL state store calls = %d, expected 0",
			stateStore.calls,
		)
	}
}

func TestSessionAccessCheckerFailsClosedWhenPostgresLookupFails(
	t *testing.T,
) {
	expectedErr := errors.New(
		"PostgreSQL unavailable",
	)

	revocationStore :=
		&sessionCheckerRevocationStore{}

	stateStore := &sessionCheckerStateStore{
		err: expectedErr,
	}

	checker, err := NewSessionAccessChecker(
		revocationStore,
		stateStore,
		&sessionCheckerClock{
			now: time.Now().UTC(),
		},
	)
	if err != nil {
		t.Fatalf(
			"NewSessionAccessChecker() returned an error: %v",
			err,
		)
	}

	_, err = checker.IsRevoked(
		context.Background(),
		"session-123",
	)
	if err == nil {
		t.Fatal(
			"IsRevoked() expected an error",
		)
	}
}

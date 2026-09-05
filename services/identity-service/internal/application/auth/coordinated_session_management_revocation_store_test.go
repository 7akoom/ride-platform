package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

type testSessionManagementRevocationTargetStore struct {
	target SessionRevocationTarget
	found  bool
	err    error

	called     bool
	identityID string
	sessionID  string
	order      *[]string
}

func (s *testSessionManagementRevocationTargetStore) FindRevocationTargetByIdentityAndSessionID(
	ctx context.Context,
	identityID string,
	sessionID string,
) (SessionRevocationTarget, bool, error) {
	s.called = true
	s.identityID = identityID
	s.sessionID = sessionID

	if s.order != nil {
		*s.order = append(
			*s.order,
			"target",
		)
	}

	return s.target, s.found, s.err
}

type testManagedSessionAccessRevocationStore struct {
	err error

	called    bool
	sessionID string
	ttl       time.Duration
	order     *[]string
}

func (s *testManagedSessionAccessRevocationStore) MarkRevoked(
	ctx context.Context,
	sessionID string,
	ttl time.Duration,
) error {
	s.called = true
	s.sessionID = sessionID
	s.ttl = ttl

	if s.order != nil {
		*s.order = append(
			*s.order,
			"access",
		)
	}

	return s.err
}

func (s *testManagedSessionAccessRevocationStore) IsRevoked(
	ctx context.Context,
	sessionID string,
) (bool, error) {
	return false, nil
}

type testManagedSessionPersistentRevocationStore struct {
	err error

	called     bool
	identityID string
	sessionID  string
	revokedAt  time.Time
	order      *[]string
}

func (s *testManagedSessionPersistentRevocationStore) RevokeSession(
	ctx context.Context,
	identityID string,
	sessionID string,
	revokedAt time.Time,
) error {
	s.called = true
	s.identityID = identityID
	s.sessionID = sessionID
	s.revokedAt = revokedAt

	if s.order != nil {
		*s.order = append(
			*s.order,
			"persistent",
		)
	}

	return s.err
}

func TestCoordinatedSessionManagementRevocationStoreRevokesAccessBeforePersistence(
	t *testing.T,
) {
	now := time.Date(
		2026,
		time.August,
		26,
		12,
		0,
		0,
		0,
		time.UTC,
	)

	sessionExpiresAt := now.Add(
		24 * time.Hour,
	)

	order := make(
		[]string,
		0,
		3,
	)

	targetStore :=
		&testSessionManagementRevocationTargetStore{
			target: SessionRevocationTarget{
				SessionID:        "session-1",
				SessionExpiresAt: sessionExpiresAt,
			},
			found: true,
			order: &order,
		}

	accessStore :=
		&testManagedSessionAccessRevocationStore{
			order: &order,
		}

	persistentStore :=
		&testManagedSessionPersistentRevocationStore{
			order: &order,
		}

	store, err :=
		NewCoordinatedSessionManagementRevocationStore(
			targetStore,
			accessStore,
			persistentStore,
		)
	if err != nil {
		t.Fatalf(
			"NewCoordinatedSessionManagementRevocationStore() returned an error: %v",
			err,
		)
	}

	err = store.RevokeSession(
		context.Background(),
		"identity-1",
		"session-1",
		now,
	)
	if err != nil {
		t.Fatalf(
			"RevokeSession() returned an error: %v",
			err,
		)
	}

	expectedOrder := []string{
		"target",
		"access",
		"persistent",
	}

	if len(order) != len(expectedOrder) {
		t.Fatalf(
			"operation order = %v, expected %v",
			order,
			expectedOrder,
		)
	}

	for index := range expectedOrder {
		if order[index] != expectedOrder[index] {
			t.Fatalf(
				"operation order = %v, expected %v",
				order,
				expectedOrder,
			)
		}
	}

	if targetStore.identityID != "identity-1" {
		t.Errorf(
			"target identity ID = %q, expected %q",
			targetStore.identityID,
			"identity-1",
		)
	}

	if targetStore.sessionID != "session-1" {
		t.Errorf(
			"target session ID = %q, expected %q",
			targetStore.sessionID,
			"session-1",
		)
	}

	if !accessStore.called {
		t.Fatal(
			"session access revocation store was not called",
		)
	}

	if accessStore.sessionID != "session-1" {
		t.Errorf(
			"access revoked session ID = %q, expected %q",
			accessStore.sessionID,
			"session-1",
		)
	}

	expectedTTL := sessionExpiresAt.Sub(
		now,
	)

	if accessStore.ttl != expectedTTL {
		t.Errorf(
			"access revocation TTL = %v, expected %v",
			accessStore.ttl,
			expectedTTL,
		)
	}

	if !persistentStore.called {
		t.Fatal(
			"persistent revocation store was not called",
		)
	}

	if persistentStore.identityID != "identity-1" {
		t.Errorf(
			"persistent identity ID = %q, expected %q",
			persistentStore.identityID,
			"identity-1",
		)
	}

	if persistentStore.sessionID != "session-1" {
		t.Errorf(
			"persistent session ID = %q, expected %q",
			persistentStore.sessionID,
			"session-1",
		)
	}

	if !persistentStore.revokedAt.Equal(
		now,
	) {
		t.Errorf(
			"persistent revokedAt = %v, expected %v",
			persistentStore.revokedAt,
			now,
		)
	}
}

func TestCoordinatedSessionManagementRevocationStoreReturnsNotFoundWithoutRevocation(
	t *testing.T,
) {
	targetStore :=
		&testSessionManagementRevocationTargetStore{
			found: false,
		}

	accessStore :=
		&testManagedSessionAccessRevocationStore{}

	persistentStore :=
		&testManagedSessionPersistentRevocationStore{}

	store, err :=
		NewCoordinatedSessionManagementRevocationStore(
			targetStore,
			accessStore,
			persistentStore,
		)
	if err != nil {
		t.Fatalf(
			"NewCoordinatedSessionManagementRevocationStore() returned an error: %v",
			err,
		)
	}

	err = store.RevokeSession(
		context.Background(),
		"identity-1",
		"missing-session",
		time.Now().UTC(),
	)

	if !errors.Is(
		err,
		ErrSessionNotFound,
	) {
		t.Fatalf(
			"RevokeSession() error = %v, expected %v",
			err,
			ErrSessionNotFound,
		)
	}

	if accessStore.called {
		t.Fatal(
			"access revocation store was called for missing session",
		)
	}

	if persistentStore.called {
		t.Fatal(
			"persistent revocation store was called for missing session",
		)
	}
}

func TestCoordinatedSessionManagementRevocationStoreDoesNotPersistWhenAccessRevocationFails(
	t *testing.T,
) {
	now := time.Now().
		UTC().
		Truncate(time.Second)

	accessFailure := errors.New(
		"Valkey unavailable",
	)

	targetStore :=
		&testSessionManagementRevocationTargetStore{
			target: SessionRevocationTarget{
				SessionID: "session-1",
				SessionExpiresAt: now.Add(
					24 * time.Hour,
				),
			},
			found: true,
		}

	accessStore :=
		&testManagedSessionAccessRevocationStore{
			err: accessFailure,
		}

	persistentStore :=
		&testManagedSessionPersistentRevocationStore{}

	store, err :=
		NewCoordinatedSessionManagementRevocationStore(
			targetStore,
			accessStore,
			persistentStore,
		)
	if err != nil {
		t.Fatalf(
			"NewCoordinatedSessionManagementRevocationStore() returned an error: %v",
			err,
		)
	}

	err = store.RevokeSession(
		context.Background(),
		"identity-1",
		"session-1",
		now,
	)

	if !errors.Is(
		err,
		accessFailure,
	) {
		t.Fatalf(
			"RevokeSession() error = %v, expected wrapped %v",
			err,
			accessFailure,
		)
	}

	if persistentStore.called {
		t.Fatal(
			"persistent revocation store was called after access revocation failure",
		)
	}
}

func TestCoordinatedSessionManagementRevocationStoreSkipsAccessMarkerForExpiredSession(
	t *testing.T,
) {
	now := time.Now().
		UTC().
		Truncate(time.Second)

	targetStore :=
		&testSessionManagementRevocationTargetStore{
			target: SessionRevocationTarget{
				SessionID: "session-expired",
				SessionExpiresAt: now.Add(
					-time.Minute,
				),
			},
			found: true,
		}

	accessStore :=
		&testManagedSessionAccessRevocationStore{}

	persistentStore :=
		&testManagedSessionPersistentRevocationStore{}

	store, err :=
		NewCoordinatedSessionManagementRevocationStore(
			targetStore,
			accessStore,
			persistentStore,
		)
	if err != nil {
		t.Fatalf(
			"NewCoordinatedSessionManagementRevocationStore() returned an error: %v",
			err,
		)
	}

	err = store.RevokeSession(
		context.Background(),
		"identity-1",
		"session-expired",
		now,
	)
	if err != nil {
		t.Fatalf(
			"RevokeSession() returned an error: %v",
			err,
		)
	}

	if accessStore.called {
		t.Fatal(
			"access revocation store was called for expired session",
		)
	}

	if !persistentStore.called {
		t.Fatal(
			"persistent revocation store was not called for expired session",
		)
	}
}

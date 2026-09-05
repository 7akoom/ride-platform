package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

type testSessionRevocationTargetStore struct {
	target           SessionRevocationTarget
	found            bool
	err              error
	called           bool
	refreshTokenHash string
	callOrder        *[]string
}

func (s *testSessionRevocationTargetStore) FindRevocationTargetByRefreshTokenHash(
	ctx context.Context,
	refreshTokenHash string,
) (SessionRevocationTarget, bool, error) {
	s.called = true
	s.refreshTokenHash = refreshTokenHash

	if s.callOrder != nil {
		*s.callOrder = append(
			*s.callOrder,
			"target",
		)
	}

	return s.target, s.found, s.err
}

type testSessionAccessRevocationStore struct {
	markErr    error
	markCalled bool
	sessionID  string
	ttl        time.Duration
	callOrder  *[]string
}

func (s *testSessionAccessRevocationStore) MarkRevoked(
	ctx context.Context,
	sessionID string,
	ttl time.Duration,
) error {
	s.markCalled = true
	s.sessionID = sessionID
	s.ttl = ttl

	if s.callOrder != nil {
		*s.callOrder = append(
			*s.callOrder,
			"access",
		)
	}

	return s.markErr
}

func (s *testSessionAccessRevocationStore) IsRevoked(
	ctx context.Context,
	sessionID string,
) (bool, error) {
	return false, nil
}

type testPersistentSessionRevocationStore struct {
	err              error
	called           bool
	refreshTokenHash string
	revokedAt        time.Time
	callOrder        *[]string
}

func (s *testPersistentSessionRevocationStore) RevokeSessionByRefreshTokenHash(
	ctx context.Context,
	refreshTokenHash string,
	revokedAt time.Time,
) error {
	s.called = true
	s.refreshTokenHash = refreshTokenHash
	s.revokedAt = revokedAt

	if s.callOrder != nil {
		*s.callOrder = append(
			*s.callOrder,
			"persistent",
		)
	}

	return s.err
}

func TestCoordinatedSessionRevocationStoreMarksAccessBeforePersistentRevocation(
	t *testing.T,
) {
	revokedAt := time.Date(
		2026,
		time.August,
		11,
		12,
		0,
		0,
		0,
		time.UTC,
	)

	sessionExpiresAt := revokedAt.Add(
		2 * time.Hour,
	)

	callOrder := []string{}

	targetStore := &testSessionRevocationTargetStore{
		target: SessionRevocationTarget{
			SessionID:        "session-123",
			SessionExpiresAt: sessionExpiresAt,
		},
		found:     true,
		callOrder: &callOrder,
	}

	accessStore := &testSessionAccessRevocationStore{
		callOrder: &callOrder,
	}

	persistentStore := &testPersistentSessionRevocationStore{
		callOrder: &callOrder,
	}

	store, err := NewCoordinatedSessionRevocationStore(
		targetStore,
		accessStore,
		persistentStore,
	)
	if err != nil {
		t.Fatalf(
			"NewCoordinatedSessionRevocationStore() returned an error: %v",
			err,
		)
	}

	err = store.RevokeByRefreshTokenHash(
		context.Background(),
		"refresh-token-hash",
		revokedAt,
	)
	if err != nil {
		t.Fatalf(
			"RevokeByRefreshTokenHash() returned an error: %v",
			err,
		)
	}

	if !targetStore.called {
		t.Fatal(
			"session revocation target store was not called",
		)
	}

	if !accessStore.markCalled {
		t.Fatal(
			"session access revocation store was not called",
		)
	}

	if !persistentStore.called {
		t.Fatal(
			"persistent session revocation store was not called",
		)
	}

	if accessStore.sessionID != "session-123" {
		t.Fatalf(
			"revoked session ID = %q, expected %q",
			accessStore.sessionID,
			"session-123",
		)
	}

	expectedTTL := sessionExpiresAt.Sub(
		revokedAt,
	)

	if accessStore.ttl != expectedTTL {
		t.Fatalf(
			"revocation TTL = %v, expected %v",
			accessStore.ttl,
			expectedTTL,
		)
	}

	if len(callOrder) != 3 ||
		callOrder[0] != "target" ||
		callOrder[1] != "access" ||
		callOrder[2] != "persistent" {
		t.Fatalf(
			"call order = %v, expected [target access persistent]",
			callOrder,
		)
	}
}

func TestCoordinatedSessionRevocationStoreDoesNothingForUnknownToken(
	t *testing.T,
) {
	targetStore := &testSessionRevocationTargetStore{
		found: false,
	}

	accessStore := &testSessionAccessRevocationStore{}

	persistentStore := &testPersistentSessionRevocationStore{}

	store, err := NewCoordinatedSessionRevocationStore(
		targetStore,
		accessStore,
		persistentStore,
	)
	if err != nil {
		t.Fatalf(
			"NewCoordinatedSessionRevocationStore() returned an error: %v",
			err,
		)
	}

	err = store.RevokeByRefreshTokenHash(
		context.Background(),
		"unknown-refresh-token-hash",
		time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf(
			"unknown refresh token returned an error: %v",
			err,
		)
	}

	if accessStore.markCalled {
		t.Fatal(
			"access revocation store was called for unknown token",
		)
	}

	if persistentStore.called {
		t.Fatal(
			"persistent revocation store was called for unknown token",
		)
	}
}

func TestCoordinatedSessionRevocationStoreDoesNotPersistWhenValkeyFails(
	t *testing.T,
) {
	expectedErr := errors.New(
		"Valkey unavailable",
	)

	now := time.Now().UTC()

	targetStore := &testSessionRevocationTargetStore{
		target: SessionRevocationTarget{
			SessionID: "session-123",
			SessionExpiresAt: now.Add(
				time.Hour,
			),
		},
		found: true,
	}

	accessStore := &testSessionAccessRevocationStore{
		markErr: expectedErr,
	}

	persistentStore := &testPersistentSessionRevocationStore{}

	store, err := NewCoordinatedSessionRevocationStore(
		targetStore,
		accessStore,
		persistentStore,
	)
	if err != nil {
		t.Fatalf(
			"NewCoordinatedSessionRevocationStore() returned an error: %v",
			err,
		)
	}

	err = store.RevokeByRefreshTokenHash(
		context.Background(),
		"refresh-token-hash",
		now,
	)
	if err == nil {
		t.Fatal(
			"RevokeByRefreshTokenHash() returned nil error when Valkey failed",
		)
	}

	if !errors.Is(err, expectedErr) {
		t.Fatalf(
			"RevokeByRefreshTokenHash() error = %v, expected wrapped %v",
			err,
			expectedErr,
		)
	}

	if persistentStore.called {
		t.Fatal(
			"persistent revocation was executed after Valkey failure",
		)
	}
}

func TestCoordinatedSessionRevocationStoreKeepsMarkerWhenPersistenceFails(
	t *testing.T,
) {
	expectedErr := errors.New(
		"PostgreSQL unavailable",
	)

	now := time.Now().UTC()

	targetStore := &testSessionRevocationTargetStore{
		target: SessionRevocationTarget{
			SessionID: "session-123",
			SessionExpiresAt: now.Add(
				time.Hour,
			),
		},
		found: true,
	}

	accessStore := &testSessionAccessRevocationStore{}

	persistentStore := &testPersistentSessionRevocationStore{
		err: expectedErr,
	}

	store, err := NewCoordinatedSessionRevocationStore(
		targetStore,
		accessStore,
		persistentStore,
	)
	if err != nil {
		t.Fatalf(
			"NewCoordinatedSessionRevocationStore() returned an error: %v",
			err,
		)
	}

	err = store.RevokeByRefreshTokenHash(
		context.Background(),
		"refresh-token-hash",
		now,
	)
	if err == nil {
		t.Fatal(
			"RevokeByRefreshTokenHash() returned nil error when persistence failed",
		)
	}

	if !errors.Is(err, expectedErr) {
		t.Fatalf(
			"RevokeByRefreshTokenHash() error = %v, expected wrapped %v",
			err,
			expectedErr,
		)
	}

	if !accessStore.markCalled {
		t.Fatal(
			"access revocation marker was not created before persistence failure",
		)
	}

	if !persistentStore.called {
		t.Fatal(
			"persistent revocation store was not called",
		)
	}
}

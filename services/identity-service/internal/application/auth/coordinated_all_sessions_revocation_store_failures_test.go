package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCoordinatedAllSessionsRevocationStoreDoesNotPersistWhenValkeyFails(
	t *testing.T,
) {
	expectedErr := errors.New(
		"Valkey unavailable",
	)

	now := time.Now().UTC()

	targetStore := &testAllSessionsRevocationTargetStore{
		target: AllSessionsRevocationTarget{
			IdentityID: "identity-123",
			Sessions: []SessionRevocationTarget{
				{
					SessionID: "session-a",
					SessionExpiresAt: now.Add(
						time.Hour,
					),
				},
				{
					SessionID: "session-b",
					SessionExpiresAt: now.Add(
						time.Hour,
					),
				},
			},
		},
		found: true,
	}

	accessStore :=
		&testAllSessionsAccessRevocationStore{
			failOnCall: 2,
			err:        expectedErr,
		}

	persistentStore :=
		&testAllSessionsPersistentRevocationStore{}

	store, err := NewCoordinatedAllSessionsRevocationStore(
		targetStore,
		accessStore,
		persistentStore,
	)
	if err != nil {
		t.Fatalf(
			"NewCoordinatedAllSessionsRevocationStore() returned an error: %v",
			err,
		)
	}

	err = store.RevokeAllByRefreshTokenHash(
		context.Background(),
		"refresh-token-hash",
		now,
	)
	if err == nil {
		t.Fatal(
			"RevokeAllByRefreshTokenHash() returned nil error when Valkey failed",
		)
	}

	if !errors.Is(err, expectedErr) {
		t.Fatalf(
			"RevokeAllByRefreshTokenHash() error = %v, expected wrapped %v",
			err,
			expectedErr,
		)
	}

	if len(accessStore.calls) != 2 {
		t.Fatalf(
			"access revocation calls = %d, expected 2",
			len(accessStore.calls),
		)
	}

	if persistentStore.called {
		t.Fatal(
			"persistent revocation ran after Valkey failure",
		)
	}
}

func TestCoordinatedAllSessionsRevocationStoreKeepsMarkersWhenPersistenceFails(
	t *testing.T,
) {
	expectedErr := errors.New(
		"PostgreSQL unavailable",
	)

	now := time.Now().UTC()

	targetStore := &testAllSessionsRevocationTargetStore{
		target: AllSessionsRevocationTarget{
			IdentityID: "identity-123",
			Sessions: []SessionRevocationTarget{
				{
					SessionID: "session-a",
					SessionExpiresAt: now.Add(
						time.Hour,
					),
				},
				{
					SessionID: "session-b",
					SessionExpiresAt: now.Add(
						time.Hour,
					),
				},
			},
		},
		found: true,
	}

	accessStore :=
		&testAllSessionsAccessRevocationStore{}

	persistentStore :=
		&testAllSessionsPersistentRevocationStore{
			err: expectedErr,
		}

	store, err := NewCoordinatedAllSessionsRevocationStore(
		targetStore,
		accessStore,
		persistentStore,
	)
	if err != nil {
		t.Fatalf(
			"NewCoordinatedAllSessionsRevocationStore() returned an error: %v",
			err,
		)
	}

	err = store.RevokeAllByRefreshTokenHash(
		context.Background(),
		"refresh-token-hash",
		now,
	)
	if err == nil {
		t.Fatal(
			"RevokeAllByRefreshTokenHash() returned nil error when persistence failed",
		)
	}

	if !errors.Is(err, expectedErr) {
		t.Fatalf(
			"RevokeAllByRefreshTokenHash() error = %v, expected wrapped %v",
			err,
			expectedErr,
		)
	}

	if len(accessStore.calls) != 2 {
		t.Fatalf(
			"access revocation calls = %d, expected 2",
			len(accessStore.calls),
		)
	}

	if !persistentStore.called {
		t.Fatal(
			"persistent revocation store was not called",
		)
	}
}

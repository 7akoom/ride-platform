package auth

import (
	"context"
	"testing"
	"time"
)

func TestCoordinatedAllSessionsRevocationStoreMarksAllSessionsBeforePersistence(
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

	callOrder := []string{}

	targetStore := &testAllSessionsRevocationTargetStore{
		target: AllSessionsRevocationTarget{
			IdentityID: "identity-123",
			Sessions: []SessionRevocationTarget{
				{
					SessionID: "session-a",
					SessionExpiresAt: revokedAt.Add(
						2 * time.Hour,
					),
				},
				{
					SessionID: "session-b",
					SessionExpiresAt: revokedAt.Add(
						30 * time.Minute,
					),
				},
			},
		},
		found:     true,
		callOrder: &callOrder,
	}

	accessStore := &testAllSessionsAccessRevocationStore{
		callOrder: &callOrder,
	}

	persistentStore :=
		&testAllSessionsPersistentRevocationStore{
			callOrder: &callOrder,
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
		revokedAt,
	)
	if err != nil {
		t.Fatalf(
			"RevokeAllByRefreshTokenHash() returned an error: %v",
			err,
		)
	}

	if !targetStore.called {
		t.Fatal(
			"all sessions revocation target store was not called",
		)
	}

	if targetStore.refreshTokenHash !=
		"refresh-token-hash" {
		t.Fatalf(
			"target lookup refresh token hash = %q, expected %q",
			targetStore.refreshTokenHash,
			"refresh-token-hash",
		)
	}

	if !targetStore.now.Equal(revokedAt) {
		t.Fatalf(
			"target lookup time = %v, expected %v",
			targetStore.now,
			revokedAt,
		)
	}

	if len(accessStore.calls) != 2 {
		t.Fatalf(
			"access revocation calls = %d, expected 2",
			len(accessStore.calls),
		)
	}

	expectedFirstTTL := 2 * time.Hour

	if accessStore.calls[0].sessionID !=
		"session-a" {
		t.Fatalf(
			"first revoked session = %q, expected %q",
			accessStore.calls[0].sessionID,
			"session-a",
		)
	}

	if accessStore.calls[0].ttl !=
		expectedFirstTTL {
		t.Fatalf(
			"first session TTL = %v, expected %v",
			accessStore.calls[0].ttl,
			expectedFirstTTL,
		)
	}

	expectedSecondTTL := 30 * time.Minute

	if accessStore.calls[1].sessionID !=
		"session-b" {
		t.Fatalf(
			"second revoked session = %q, expected %q",
			accessStore.calls[1].sessionID,
			"session-b",
		)
	}

	if accessStore.calls[1].ttl !=
		expectedSecondTTL {
		t.Fatalf(
			"second session TTL = %v, expected %v",
			accessStore.calls[1].ttl,
			expectedSecondTTL,
		)
	}

	if !persistentStore.called {
		t.Fatal(
			"persistent revocation store was not called",
		)
	}

	if persistentStore.identityID !=
		"identity-123" {
		t.Fatalf(
			"persistent identity ID = %q, expected %q",
			persistentStore.identityID,
			"identity-123",
		)
	}

	if len(persistentStore.sessionIDs) != 2 ||
		persistentStore.sessionIDs[0] != "session-a" ||
		persistentStore.sessionIDs[1] != "session-b" {
		t.Fatalf(
			"persistent session IDs = %v, expected [session-a session-b]",
			persistentStore.sessionIDs,
		)
	}

	expectedOrder := []string{
		"target",
		"access:session-a",
		"access:session-b",
		"persistent",
	}

	if len(callOrder) != len(expectedOrder) {
		t.Fatalf(
			"call order = %v, expected %v",
			callOrder,
			expectedOrder,
		)
	}

	for i := range expectedOrder {
		if callOrder[i] != expectedOrder[i] {
			t.Fatalf(
				"call order = %v, expected %v",
				callOrder,
				expectedOrder,
			)
		}
	}
}

func TestCoordinatedAllSessionsRevocationStoreSkipsAccessMarkerForExpiredSession(
	t *testing.T,
) {
	revokedAt := time.Now().
		UTC().
		Truncate(time.Second)

	targetStore := &testAllSessionsRevocationTargetStore{
		target: AllSessionsRevocationTarget{
			IdentityID: "identity-123",
			Sessions: []SessionRevocationTarget{
				{
					SessionID: "expired-session",
					SessionExpiresAt: revokedAt.Add(
						-time.Minute,
					),
				},
			},
		},
		found: true,
	}

	accessStore :=
		&testAllSessionsAccessRevocationStore{}

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
		revokedAt,
	)
	if err != nil {
		t.Fatalf(
			"RevokeAllByRefreshTokenHash() returned an error: %v",
			err,
		)
	}

	if len(accessStore.calls) != 0 {
		t.Fatalf(
			"access revocation calls = %d, expected 0",
			len(accessStore.calls),
		)
	}

	if !persistentStore.called {
		t.Fatal(
			"persistent revocation store was not called",
		)
	}

	if persistentStore.identityID != "identity-123" {
		t.Fatalf(
			"persistent identity ID = %q, expected %q",
			persistentStore.identityID,
			"identity-123",
		)
	}

	if len(persistentStore.sessionIDs) != 1 ||
		persistentStore.sessionIDs[0] != "expired-session" {
		t.Fatalf(
			"persistent session IDs = %v, expected [expired-session]",
			persistentStore.sessionIDs,
		)
	}
}

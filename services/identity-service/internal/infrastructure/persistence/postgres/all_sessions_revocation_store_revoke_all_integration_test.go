//go:build integration

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

type integrationSessionAccessRevocationStore struct {
	revokedSessions map[string]time.Duration
}

func newIntegrationSessionAccessRevocationStore() *integrationSessionAccessRevocationStore {
	return &integrationSessionAccessRevocationStore{
		revokedSessions: make(
			map[string]time.Duration,
		),
	}
}

func (s *integrationSessionAccessRevocationStore) MarkRevoked(
	ctx context.Context,
	sessionID string,
	ttl time.Duration,
) error {
	s.revokedSessions[sessionID] = ttl

	return nil
}

func (s *integrationSessionAccessRevocationStore) IsRevoked(
	ctx context.Context,
	sessionID string,
) (bool, error) {
	_, revoked := s.revokedSessions[sessionID]

	return revoked, nil
}

func TestAllSessionsRevocationStoreRevokesAllSessionsAndOldTokenCannotRevokeNewSession(
	t *testing.T,
) {
	fixture := newAllSessionsRevocationTestFixture(
		t,
		"+9647500000052",
	)

	cleanupIdentitySessionsRevokedOutboxEvents(
		t,
		fixture,
	)

	firstTokenHash := strings.Repeat(
		"f",
		64,
	)

	secondTokenHash := strings.Repeat(
		"1",
		64,
	)

	firstSessionID :=
		fixture.createSessionWithRefreshToken(
			fixture.now,
			firstTokenHash,
		)

	secondSessionID :=
		fixture.createSessionWithRefreshToken(
			fixture.now.Add(time.Second),
			secondTokenHash,
		)

	persistentStore := NewAllSessionsRevocationStore(
		fixture.pool,
	)

	accessRevocationStore :=
		newIntegrationSessionAccessRevocationStore()

	store, err := auth.NewCoordinatedAllSessionsRevocationStore(
		persistentStore,
		accessRevocationStore,
		persistentStore,
	)
	if err != nil {
		t.Fatalf(
			"NewCoordinatedAllSessionsRevocationStore() returned an error: %v",
			err,
		)
	}

	logoutAllAt := fixture.now.Add(
		time.Minute,
	)

	err = store.RevokeAllByRefreshTokenHash(
		fixture.ctx,
		firstTokenHash,
		logoutAllAt,
	)
	if err != nil {
		t.Fatalf(
			"RevokeAllByRefreshTokenHash() returned an error: %v",
			err,
		)
	}

	totalSessions, revokedSessions, activeSessions :=
		fixture.sessionCounts()

	if totalSessions != 2 {
		t.Fatalf(
			"session count = %d, expected 2",
			totalSessions,
		)
	}

	if revokedSessions != 2 {
		t.Fatalf(
			"revoked session count = %d, expected 2",
			revokedSessions,
		)
	}

	if activeSessions != 0 {
		t.Fatalf(
			"active session count = %d, expected 0",
			activeSessions,
		)
	}

	totalRefreshTokens, revokedRefreshTokens, activeRefreshTokens :=
		fixture.refreshTokenCounts()

	if totalRefreshTokens != 2 {
		t.Fatalf(
			"refresh token count = %d, expected 2",
			totalRefreshTokens,
		)
	}

	if revokedRefreshTokens != 2 {
		t.Fatalf(
			"revoked refresh token count = %d, expected 2",
			revokedRefreshTokens,
		)
	}

	if activeRefreshTokens != 0 {
		t.Fatalf(
			"active refresh token count = %d, expected 0",
			activeRefreshTokens,
		)
	}

	firstAccessRevoked, err :=
		accessRevocationStore.IsRevoked(
			fixture.ctx,
			firstSessionID,
		)
	if err != nil {
		t.Fatalf(
			"check first session access revocation: %v",
			err,
		)
	}

	if !firstAccessRevoked {
		t.Fatal(
			"first session access was not revoked",
		)
	}

	secondAccessRevoked, err :=
		accessRevocationStore.IsRevoked(
			fixture.ctx,
			secondSessionID,
		)
	if err != nil {
		t.Fatalf(
			"check second session access revocation: %v",
			err,
		)
	}

	if !secondAccessRevoked {
		t.Fatal(
			"second session access was not revoked",
		)
	}

	if accessRevocationStore.revokedSessions[firstSessionID] <= 0 {
		t.Fatal(
			"first session access revocation TTL is not positive",
		)
	}

	if accessRevocationStore.revokedSessions[secondSessionID] <= 0 {
		t.Fatal(
			"second session access revocation TTL is not positive",
		)
	}

	assertIdentitySessionsRevokedOutboxEvent(
		t,
		fixture,
		[]string{
			firstSessionID,
			secondSessionID,
		},
		logoutAllAt,
	)

	eventCount := countIdentitySessionsRevokedOutboxEvents(
		t,
		fixture,
	)

	if eventCount != 1 {
		t.Fatalf(
			"sessions revoked outbox event count = %d, want 1",
			eventCount,
		)
	}

	newSessionCreatedAt := logoutAllAt.Add(
		time.Minute,
	)

	newTokenHash := strings.Repeat(
		"2",
		64,
	)

	newSessionID :=
		fixture.createSessionWithRefreshToken(
			newSessionCreatedAt,
			newTokenHash,
		)

	err = store.RevokeAllByRefreshTokenHash(
		fixture.ctx,
		firstTokenHash,
		newSessionCreatedAt.Add(time.Minute),
	)
	if err != nil {
		t.Fatalf(
			"old refresh token returned an error: %v",
			err,
		)
	}

	newState := fixture.readRevocationState(
		newSessionID,
		newTokenHash,
	)

	if newState.sessionRevokedAt != nil {
		t.Fatal(
			"old refresh token revoked a newly created session",
		)
	}

	if newState.tokenRevokedAt != nil {
		t.Fatal(
			"old refresh token revoked a newly created refresh token",
		)
	}

	newAccessRevoked, err :=
		accessRevocationStore.IsRevoked(
			fixture.ctx,
			newSessionID,
		)
	if err != nil {
		t.Fatalf(
			"check new session access revocation: %v",
			err,
		)
	}

	if newAccessRevoked {
		t.Fatal(
			"old refresh token revoked access for newly created session",
		)
	}

	eventCount = countIdentitySessionsRevokedOutboxEvents(
		t,
		fixture,
	)

	if eventCount != 1 {
		t.Fatalf(
			"sessions revoked outbox event count after old token reuse = %d, want 1",
			eventCount,
		)
	}
}

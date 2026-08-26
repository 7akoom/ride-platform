//go:build integration

package postgres

import (
	"strings"
	"testing"
	"time"
)

func TestAllSessionsRevocationStoreRevokesAllSessionsAndOldTokenCannotRevokeNewSession(
	t *testing.T,
) {
	fixture := newAllSessionsRevocationTestFixture(
		t,
		"+9647500000052",
	)

	firstTokenHash := strings.Repeat(
		"f",
		64,
	)

	secondTokenHash := strings.Repeat(
		"1",
		64,
	)

	fixture.createSessionWithRefreshToken(
		fixture.now,
		firstTokenHash,
	)

	fixture.createSessionWithRefreshToken(
		fixture.now.Add(time.Second),
		secondTokenHash,
	)

	store := NewAllSessionsRevocationStore(
		fixture.pool,
	)

	logoutAllAt := fixture.now.Add(
		time.Minute,
	)

	err := store.RevokeAllByRefreshTokenHash(
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
}

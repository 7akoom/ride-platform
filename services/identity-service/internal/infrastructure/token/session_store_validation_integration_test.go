//go:build integration

package token

import (
	"strings"
	"testing"
	"time"
)

func TestSessionStoreCreateRejectsInvalidRefreshTokenHash(
	t *testing.T,
) {
	fixture := newSessionStoreIntegrationFixture(
		t,
		"+9647500000004",
	)

	sessionID := fixture.generateSessionID()

	store := NewSessionStore(
		fixture.pool,
	)

	now := time.Now().UTC()

	challengeID := "otp-" + sessionID

	fixture.createOTPChallenge(
		challengeID,
		strings.Repeat("c", 64),
		now.Add(5*time.Minute),
	)

	verifiedAt := time.Now().UTC()

	invalidRefreshTokenHash := strings.Repeat(
		"A",
		64,
	)

	_, err := store.Create(
		fixture.ctx,
		SessionCreationInput{
			ChallengeID:           challengeID,
			VerifiedAt:            verifiedAt,
			SessionID:             sessionID,
			IdentityID:            fixture.identityID,
			SessionExpiresAt:      now.Add(24 * time.Hour),
			RefreshTokenHash:      invalidRefreshTokenHash,
			RefreshTokenExpiresAt: now.Add(23 * time.Hour),
		},
	)

	if err == nil {
		t.Fatal(
			"Create() accepted an invalid refresh token hash",
		)
	}

	sessionCount := fixture.countAuthSessions()

	if sessionCount != 0 {
		t.Fatalf(
			"database contains %d sessions, expected 0 after rejected refresh token hash",
			sessionCount,
		)
	}

	rolledBackVerifiedAt :=
		fixture.challengeVerifiedAt(
			challengeID,
		)

	if rolledBackVerifiedAt != nil {
		t.Fatal(
			"OTP challenge remained verified after session transaction rollback",
		)
	}
}

//go:build integration

package postgres

import (
	"strings"
	"testing"
	"time"
)

func TestCleanupStoreDeletesOnlyRecordsPastRetention(
	t *testing.T,
) {
	const phoneNumber = "+9647500000071"

	fixture := newCleanupStoreIntegrationFixture(
		t,
		phoneNumber,
	)

	oldOTPRequestTime := fixture.now.Add(
		-48 * time.Hour,
	)

	recentOTPRequestTime := fixture.now.Add(
		-12 * time.Hour,
	)

	fixture.createOTPRequestEvents(
		oldOTPRequestTime,
		recentOTPRequestTime,
	)

	oldChallengeExpiresAt := fixture.now.Add(
		-48 * time.Hour,
	)

	recentChallengeExpiresAt := fixture.now.Add(
		-12 * time.Hour,
	)

	fixture.createOTPChallenges(
		oldChallengeExpiresAt,
		recentChallengeExpiresAt,
	)

	identityID := fixture.createIdentity(
		fixture.now.Add(-90 * 24 * time.Hour),
	)

	oldRevokedAt := fixture.now.Add(
		-40 * 24 * time.Hour,
	)

	sessions := []cleanupStoreSessionFixture{
		{
			tokenHash: strings.Repeat("7", 64),
			createdAt: fixture.now.Add(
				-70 * 24 * time.Hour,
			),
			expiresAt: fixture.now.Add(
				-60 * 24 * time.Hour,
			),
		},
		{
			tokenHash: strings.Repeat("8", 64),
			createdAt: fixture.now.Add(
				-60 * 24 * time.Hour,
			),
			expiresAt: fixture.now.Add(
				10 * 24 * time.Hour,
			),
			revokedAt: &oldRevokedAt,
		},
		{
			tokenHash: strings.Repeat("9", 64),
			createdAt: fixture.now.Add(
				-10 * 24 * time.Hour,
			),
			expiresAt: fixture.now.Add(
				-24 * time.Hour,
			),
		},
	}

	for _, session := range sessions {
		fixture.createSessionWithRefreshToken(
			identityID,
			session,
		)
	}

	store := NewCleanupStore(
		fixture.pool,
	)

	result, err := store.Cleanup(
		fixture.ctx,
		fixture.now,
		24*time.Hour,
		24*time.Hour,
		30*24*time.Hour,
	)
	if err != nil {
		t.Fatalf(
			"Cleanup() returned an error: %v",
			err,
		)
	}

	if result.OTPRequestEventsDeleted < 1 {
		t.Fatalf(
			"OTPRequestEventsDeleted = %d, expected at least 1",
			result.OTPRequestEventsDeleted,
		)
	}

	if result.OTPChallengesDeleted < 1 {
		t.Fatalf(
			"OTPChallengesDeleted = %d, expected at least 1",
			result.OTPChallengesDeleted,
		)
	}

	if result.AuthSessionsDeleted < 2 {
		t.Fatalf(
			"AuthSessionsDeleted = %d, expected at least 2",
			result.AuthSessionsDeleted,
		)
	}

	if remaining := fixture.remainingOTPRequestEvents(); remaining != 1 {
		t.Fatalf(
			"remaining OTP request events = %d, expected 1",
			remaining,
		)
	}

	if remaining := fixture.remainingOTPChallenges(); remaining != 1 {
		t.Fatalf(
			"remaining OTP challenges = %d, expected 1",
			remaining,
		)
	}

	if remaining := fixture.remainingSessions(identityID); remaining != 1 {
		t.Fatalf(
			"remaining authentication sessions = %d, expected 1",
			remaining,
		)
	}

	if remaining := fixture.remainingRefreshTokens(identityID); remaining != 1 {
		t.Fatalf(
			"remaining refresh tokens = %d, expected 1",
			remaining,
		)
	}

	retainedTokenHash := strings.Repeat(
		"9",
		64,
	)

	if !fixture.refreshTokenExists(retainedTokenHash) {
		t.Fatal(
			"recent refresh token was deleted unexpectedly",
		)
	}
}

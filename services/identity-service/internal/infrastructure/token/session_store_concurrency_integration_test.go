//go:build integration

package token

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

func TestSessionStoreCreateAllowsOnlyOneConcurrentChallengeClaim(
	t *testing.T,
) {
	fixture := newSessionStoreIntegrationFixture(
		t,
		"+9647500000006",
	)

	firstSessionID := fixture.generateSessionID()
	secondSessionID := fixture.generateSessionID()

	challengeID :=
		"otp-concurrency-" + fixture.identityID

	now := time.Now().UTC()

	fixture.createOTPChallenge(
		challengeID,
		strings.Repeat("d", 64),
		now.Add(5*time.Minute),
	)

	verifiedAt := time.Now().UTC()

	store := NewSessionStore(
		fixture.pool,
	)

	sessionExpiresAt := verifiedAt.Add(
		30 * 24 * time.Hour,
	)

	refreshTokenExpiresAt := verifiedAt.Add(
		29 * 24 * time.Hour,
	)

	firstInput := SessionCreationInput{
		ChallengeID:      challengeID,
		VerifiedAt:       verifiedAt,
		SessionID:        firstSessionID,
		IdentityID:       fixture.identityID,
		SessionExpiresAt: sessionExpiresAt,
		RefreshTokenHash: HashRefreshToken(
			"rt_concurrent_first",
		),
		RefreshTokenExpiresAt: refreshTokenExpiresAt,
	}

	secondInput := SessionCreationInput{
		ChallengeID:      challengeID,
		VerifiedAt:       verifiedAt,
		SessionID:        secondSessionID,
		IdentityID:       fixture.identityID,
		SessionExpiresAt: sessionExpiresAt,
		RefreshTokenHash: HashRefreshToken(
			"rt_concurrent_second",
		),
		RefreshTokenExpiresAt: refreshTokenExpiresAt,
	}

	start := make(chan struct{})
	results := make(chan error, 2)

	runCreate := func(
		input SessionCreationInput,
	) {
		<-start

		_, createErr := store.Create(
			fixture.ctx,
			input,
		)

		results <- createErr
	}

	go runCreate(firstInput)
	go runCreate(secondInput)

	close(start)

	firstErr := <-results
	secondErr := <-results

	successCount := 0
	challengeUsedCount := 0

	for _, createErr := range []error{
		firstErr,
		secondErr,
	} {
		switch {
		case createErr == nil:
			successCount++

		case errors.Is(
			createErr,
			auth.ErrChallengeUsed,
		):
			challengeUsedCount++

		default:
			t.Fatalf(
				"concurrent Create() returned unexpected error: %v",
				createErr,
			)
		}
	}

	if successCount != 1 {
		t.Fatalf(
			"successful concurrent Create() calls = %d, expected 1",
			successCount,
		)
	}

	if challengeUsedCount != 1 {
		t.Fatalf(
			"ErrChallengeUsed results = %d, expected 1",
			challengeUsedCount,
		)
	}

	sessionCount := fixture.countAuthSessions()

	if sessionCount != 1 {
		t.Fatalf(
			"database contains %d sessions, expected 1",
			sessionCount,
		)
	}

	refreshTokenCount := fixture.countRefreshTokens()

	if refreshTokenCount != 1 {
		t.Fatalf(
			"database contains %d refresh tokens, expected 1",
			refreshTokenCount,
		)
	}

	storedVerifiedAt :=
		fixture.challengeVerifiedAt(
			challengeID,
		)

	if storedVerifiedAt == nil {
		t.Fatal(
			"OTP challenge was not marked verified",
		)
	}

	verificationTimeDifference :=
		storedVerifiedAt.Sub(verifiedAt)

	if verificationTimeDifference < 0 {
		verificationTimeDifference =
			-verificationTimeDifference
	}

	if verificationTimeDifference > time.Microsecond {
		t.Fatalf(
			"OTP challenge verification time = %v, expected approximately %v",
			*storedVerifiedAt,
			verifiedAt,
		)
	}
}

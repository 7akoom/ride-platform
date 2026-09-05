//go:build integration

package token

import (
	"strings"
	"testing"
	"time"
)

func TestSessionStoreCreateIsAtomic(t *testing.T) {
	fixture := newSessionStoreIntegrationFixture(
		t,
		"+9647500000003",
	)

	firstSessionID := fixture.generateSessionID()
	secondSessionID := fixture.generateSessionID()

	store := NewSessionStore(
		fixture.pool,
	)

	refreshToken := "rt_session-store-integration-test"
	refreshTokenHash := HashRefreshToken(refreshToken)

	now := time.Now().UTC()

	firstChallengeID := "otp-" + firstSessionID
	secondChallengeID := "otp-" + secondSessionID

	fixture.createOTPChallenge(
		firstChallengeID,
		strings.Repeat("a", 64),
		now.Add(5*time.Minute),
	)

	fixture.createOTPChallenge(
		secondChallengeID,
		strings.Repeat("b", 64),
		now.Add(5*time.Minute),
	)

	verifiedAt := time.Now().UTC()

	sessionExpiresAt := now.Add(
		30 * 24 * time.Hour,
	)

	refreshTokenExpiresAt := now.Add(
		29 * 24 * time.Hour,
	)

	issuedSession, err := store.Create(
		fixture.ctx,
		SessionCreationInput{
			ChallengeID:           firstChallengeID,
			VerifiedAt:            verifiedAt,
			SessionID:             firstSessionID,
			IdentityID:            fixture.identityID,
			SessionExpiresAt:      sessionExpiresAt,
			RefreshTokenHash:      refreshTokenHash,
			RefreshTokenExpiresAt: refreshTokenExpiresAt,
		},
	)
	if err != nil {
		t.Fatalf(
			"Create() returned an error: %v",
			err,
		)
	}

	if issuedSession.SessionID != firstSessionID {
		t.Fatalf(
			"Create() returned session ID %q, expected %q",
			issuedSession.SessionID,
			firstSessionID,
		)
	}

	if issuedSession.RefreshTokenID == "" {
		t.Fatal(
			"Create() returned an empty refresh token ID",
		)
	}

	var storedIdentityID string
	var storedTokenHash string

	err = fixture.pool.QueryRow(
		fixture.ctx,
		`
			SELECT
				s.identity_id::text,
				rt.token_hash
			FROM auth_sessions AS s
			INNER JOIN refresh_tokens AS rt
				ON rt.session_id = s.id
			WHERE s.id = $1::uuid
			  AND rt.id = $2::uuid
		`,
		issuedSession.SessionID,
		issuedSession.RefreshTokenID,
	).Scan(
		&storedIdentityID,
		&storedTokenHash,
	)
	if err != nil {
		t.Fatalf(
			"query created session and refresh token: %v",
			err,
		)
	}

	if storedIdentityID != fixture.identityID {
		t.Fatalf(
			"stored identity ID is %q, expected %q",
			storedIdentityID,
			fixture.identityID,
		)
	}

	if storedTokenHash != refreshTokenHash {
		t.Fatal(
			"stored refresh token hash does not match expected hash",
		)
	}

	_, err = store.Create(
		fixture.ctx,
		SessionCreationInput{
			ChallengeID:           secondChallengeID,
			VerifiedAt:            verifiedAt,
			SessionID:             secondSessionID,
			IdentityID:            fixture.identityID,
			SessionExpiresAt:      sessionExpiresAt,
			RefreshTokenHash:      refreshTokenHash,
			RefreshTokenExpiresAt: refreshTokenExpiresAt,
		},
	)

	if err == nil {
		t.Fatal(
			"Create() accepted a duplicate refresh token hash",
		)
	}

	sessionCount := fixture.countAuthSessions()

	if sessionCount != 1 {
		t.Fatalf(
			"database contains %d sessions, expected 1 after failed transaction",
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

	rolledBackVerifiedAt :=
		fixture.challengeVerifiedAt(
			secondChallengeID,
		)

	if rolledBackVerifiedAt != nil {
		t.Fatal(
			"second OTP challenge remained verified after failed session transaction",
		)
	}
}

//go:build integration

package postgres

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/database"
)

func TestCleanupStoreDeletesOnlyRecordsPastRetention(
	t *testing.T,
) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("DATABASE_URL is required for integration test")
	}

	ctx := context.Background()

	pool, err := database.NewPostgresPool(
		ctx,
		databaseURL,
	)
	if err != nil {
		t.Fatalf(
			"connect to PostgreSQL: %v",
			err,
		)
	}

	t.Cleanup(pool.Close)

	const phoneNumber = "+9647500000071"

	for _, query := range []string{
		`
				DELETE FROM otp_request_events
				WHERE identifier_type = 'phone'
				AND normalized_value = $1
				AND purpose = 'login'
				AND target_identity_id IS NULL
			`,
		`
				DELETE FROM otp_challenges
				WHERE identifier_type = 'phone'
				AND normalized_value = $1
				AND purpose = 'login'
				AND target_identity_id IS NULL
			`,
		`
				DELETE FROM identities AS i
				USING identity_identifiers AS ii
				WHERE ii.identity_id = i.id
				AND ii.identifier_type = 'phone'
				AND ii.normalized_value = $1
			`,
	} {
		_, err = pool.Exec(
			ctx,
			query,
			phoneNumber,
		)
		if err != nil {
			t.Fatalf(
				"clean existing integration test data: %v",
				err,
			)
		}
	}
	if err != nil {
		t.Fatalf(
			"clean existing integration test data: %v",
			err,
		)
	}

	t.Cleanup(func() {
		for _, query := range []string{
			`
			DELETE FROM otp_request_events
			WHERE identifier_type = 'phone'
			AND normalized_value = $1
			AND purpose = 'login'
			AND target_identity_id IS NULL
		`,
			`
			DELETE FROM otp_challenges
			WHERE identifier_type = 'phone'
			AND normalized_value = $1
			AND purpose = 'login'
			AND target_identity_id IS NULL
		`,
			`
			DELETE FROM identities AS i
			USING identity_identifiers AS ii
			WHERE ii.identity_id = i.id
			AND ii.identifier_type = 'phone'
			AND ii.normalized_value = $1
		`,
		} {
			_, cleanupErr := pool.Exec(
				context.Background(),
				query,
				phoneNumber,
			)
			if cleanupErr != nil {
				t.Errorf(
					"clean integration test data: %v",
					cleanupErr,
				)
			}
		}
	})

	now := time.Now().
		UTC().
		Truncate(time.Microsecond)

	oldOTPRequestTime := now.Add(
		-48 * time.Hour,
	)

	recentOTPRequestTime := now.Add(
		-12 * time.Hour,
	)

	for _, requestedAt := range []time.Time{
		oldOTPRequestTime,
		recentOTPRequestTime,
	} {
		_, err = pool.Exec(
			ctx,
			`
			INSERT INTO otp_request_events (
				identifier_type,
				normalized_value,
				purpose,
				target_identity_id,
				requested_at,
				created_at
			)
			VALUES (
				'phone',
				$1,
				'login',
				NULL,
				$2,
				$2
			)
		`,
			phoneNumber,
			requestedAt,
		)
		if err != nil {
			t.Fatalf(
				"create OTP request event: %v",
				err,
			)
		}
	}

	oldChallengeExpiresAt := now.Add(
		-48 * time.Hour,
	)

	recentChallengeExpiresAt := now.Add(
		-12 * time.Hour,
	)

	_, err = pool.Exec(
		ctx,
		`
			INSERT INTO otp_challenges (
				id,
				identifier_type,
				normalized_value,
				purpose,
				target_identity_id,
				code_hash,
				expires_at,
				created_at
			)
			VALUES
			(
				'cleanup-old-challenge',
				'phone',
				$1,
				'login',
				NULL,
				$2,
				$3,
				$4
			),
			(
				'cleanup-recent-challenge',
				'phone',
				$1,
				'login',
				NULL,
				$2,
				$5,
				$6
			)
		`,
		phoneNumber,
		strings.Repeat("a", 64),
		oldChallengeExpiresAt,
		oldChallengeExpiresAt.Add(-5*time.Minute),
		recentChallengeExpiresAt,
		recentChallengeExpiresAt.Add(-5*time.Minute),
	)
	if err != nil {
		t.Fatalf(
			"create OTP challenges: %v",
			err,
		)
	}

	var identityID string

	identityCreatedAt := now.Add(-90 * 24 * time.Hour)

	err = pool.QueryRow(
		ctx,
		`
			WITH created_identity AS (
				INSERT INTO identities (
					created_at,
					updated_at
				)
				VALUES ($1, $1)
				RETURNING id
			)
			INSERT INTO identity_identifiers (
				identity_id,
				identifier_type,
				normalized_value,
				verified_at,
				created_at,
				updated_at
			)
			SELECT
				id,
				'phone',
				$2,
				$1,
				$1,
				$1
			FROM created_identity
			RETURNING identity_id::text
		`,
		identityCreatedAt,
		phoneNumber,
	).Scan(
		&identityID,
	)
	if err != nil {
		t.Fatalf(
			"create identity: %v",
			err,
		)
	}

	type sessionFixture struct {
		tokenHash string
		createdAt time.Time
		expiresAt time.Time
		revokedAt *time.Time
	}

	oldRevokedAt := now.Add(
		-40 * 24 * time.Hour,
	)

	sessions := []sessionFixture{
		{
			tokenHash: strings.Repeat("7", 64),
			createdAt: now.Add(-70 * 24 * time.Hour),
			expiresAt: now.Add(-60 * 24 * time.Hour),
		},
		{
			tokenHash: strings.Repeat("8", 64),
			createdAt: now.Add(-60 * 24 * time.Hour),
			expiresAt: now.Add(10 * 24 * time.Hour),
			revokedAt: &oldRevokedAt,
		},
		{
			tokenHash: strings.Repeat("9", 64),
			createdAt: now.Add(-10 * 24 * time.Hour),
			expiresAt: now.Add(-24 * time.Hour),
		},
	}

	for _, fixture := range sessions {
		var sessionID string

		err = pool.QueryRow(
			ctx,
			`
				INSERT INTO auth_sessions (
					identity_id,
					expires_at,
					revoked_at,
					created_at,
					updated_at
				)
				VALUES (
					$1::uuid,
					$2,
					$3,
					$4,
					$4
				)
				RETURNING id::text
			`,
			identityID,
			fixture.expiresAt,
			fixture.revokedAt,
			fixture.createdAt,
		).Scan(
			&sessionID,
		)
		if err != nil {
			t.Fatalf(
				"create authentication session: %v",
				err,
			)
		}

		refreshExpiresAt := fixture.expiresAt.Add(
			-time.Hour,
		)

		if refreshExpiresAt.Before(
			fixture.createdAt,
		) {
			refreshExpiresAt = fixture.createdAt.Add(
				time.Hour,
			)
		}

		_, err = pool.Exec(
			ctx,
			`
				INSERT INTO refresh_tokens (
					session_id,
					token_hash,
					expires_at,
					created_at
				)
				VALUES (
					$1::uuid,
					$2,
					$3,
					$4
				)
			`,
			sessionID,
			fixture.tokenHash,
			refreshExpiresAt,
			fixture.createdAt,
		)
		if err != nil {
			t.Fatalf(
				"create refresh token: %v",
				err,
			)
		}
	}

	store := NewCleanupStore(
		pool,
	)

	result, err := store.Cleanup(
		ctx,
		now,
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

	var remainingOTPRequestEvents int

	err = pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM otp_request_events
			WHERE identifier_type = 'phone'
			AND normalized_value = $1
			AND purpose = 'login'
			AND target_identity_id IS NULL
		`,
		phoneNumber,
	).Scan(
		&remainingOTPRequestEvents,
	)
	if err != nil {
		t.Fatalf(
			"count remaining OTP request events: %v",
			err,
		)
	}

	if remainingOTPRequestEvents != 1 {
		t.Fatalf(
			"remaining OTP request events = %d, expected 1",
			remainingOTPRequestEvents,
		)
	}

	var remainingOTPChallenges int

	err = pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM otp_challenges
			WHERE identifier_type = 'phone'
			AND normalized_value = $1
			AND purpose = 'login'
			AND target_identity_id IS NULL
		`,
		phoneNumber,
	).Scan(
		&remainingOTPChallenges,
	)
	if err != nil {
		t.Fatalf(
			"count remaining OTP challenges: %v",
			err,
		)
	}

	if remainingOTPChallenges != 1 {
		t.Fatalf(
			"remaining OTP challenges = %d, expected 1",
			remainingOTPChallenges,
		)
	}

	var remainingSessions int

	err = pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM auth_sessions
			WHERE identity_id = $1::uuid
		`,
		identityID,
	).Scan(
		&remainingSessions,
	)
	if err != nil {
		t.Fatalf(
			"count remaining authentication sessions: %v",
			err,
		)
	}

	if remainingSessions != 1 {
		t.Fatalf(
			"remaining authentication sessions = %d, expected 1",
			remainingSessions,
		)
	}

	var remainingRefreshTokens int

	err = pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM refresh_tokens rt
			JOIN auth_sessions s
				ON s.id = rt.session_id
			WHERE s.identity_id = $1::uuid
		`,
		identityID,
	).Scan(
		&remainingRefreshTokens,
	)
	if err != nil {
		t.Fatalf(
			"count remaining refresh tokens: %v",
			err,
		)
	}

	if remainingRefreshTokens != 1 {
		t.Fatalf(
			"remaining refresh tokens = %d, expected 1",
			remainingRefreshTokens,
		)
	}

	var retainedTokenExists bool

	err = pool.QueryRow(
		ctx,
		`
			SELECT EXISTS (
				SELECT 1
				FROM refresh_tokens
				WHERE token_hash = $1
			)
		`,
		strings.Repeat("9", 64),
	).Scan(
		&retainedTokenExists,
	)
	if err != nil {
		t.Fatalf(
			"query retained refresh token: %v",
			err,
		)
	}

	if !retainedTokenExists {
		t.Fatal(
			"recent refresh token was deleted unexpectedly",
		)
	}
}

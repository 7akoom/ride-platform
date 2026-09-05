//go:build integration

package postgres

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/database"
	"github.com/jackc/pgx/v5/pgxpool"
)

type cleanupStoreIntegrationFixture struct {
	t           *testing.T
	ctx         context.Context
	pool        *pgxpool.Pool
	phoneNumber string
	now         time.Time
}

type cleanupStoreSessionFixture struct {
	tokenHash string
	createdAt time.Time
	expiresAt time.Time
	revokedAt *time.Time
}

func newCleanupStoreIntegrationFixture(
	t *testing.T,
	phoneNumber string,
) *cleanupStoreIntegrationFixture {
	t.Helper()

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

	fixture := &cleanupStoreIntegrationFixture{
		t:           t,
		ctx:         ctx,
		pool:        pool,
		phoneNumber: phoneNumber,
		now: time.Now().
			UTC().
			Truncate(time.Microsecond),
	}

	fixture.cleanup()

	t.Cleanup(func() {
		fixture.cleanup()
	})

	return fixture
}

func (f *cleanupStoreIntegrationFixture) cleanup() {
	f.t.Helper()

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
		_, err := f.pool.Exec(
			context.Background(),
			query,
			f.phoneNumber,
		)
		if err != nil {
			f.t.Fatalf(
				"clean integration test data: %v",
				err,
			)
		}
	}
}

func (f *cleanupStoreIntegrationFixture) createOTPRequestEvents(
	requestedAt ...time.Time,
) {
	f.t.Helper()

	for _, requestedAt := range requestedAt {
		_, err := f.pool.Exec(
			f.ctx,
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
			f.phoneNumber,
			requestedAt,
		)
		if err != nil {
			f.t.Fatalf(
				"create OTP request event: %v",
				err,
			)
		}
	}
}

func (f *cleanupStoreIntegrationFixture) createOTPChallenges(
	oldExpiresAt time.Time,
	recentExpiresAt time.Time,
) {
	f.t.Helper()

	_, err := f.pool.Exec(
		f.ctx,
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
		f.phoneNumber,
		strings.Repeat("a", 64),
		oldExpiresAt,
		oldExpiresAt.Add(-5*time.Minute),
		recentExpiresAt,
		recentExpiresAt.Add(-5*time.Minute),
	)
	if err != nil {
		f.t.Fatalf(
			"create OTP challenges: %v",
			err,
		)
	}
}

func (f *cleanupStoreIntegrationFixture) createIdentity(
	createdAt time.Time,
) string {
	f.t.Helper()

	var identityID string

	err := f.pool.QueryRow(
		f.ctx,
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
		createdAt,
		f.phoneNumber,
	).Scan(
		&identityID,
	)
	if err != nil {
		f.t.Fatalf(
			"create identity: %v",
			err,
		)
	}

	return identityID
}

func (f *cleanupStoreIntegrationFixture) createSessionWithRefreshToken(
	identityID string,
	session cleanupStoreSessionFixture,
) {
	f.t.Helper()

	var sessionID string

	err := f.pool.QueryRow(
		f.ctx,
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
		session.expiresAt,
		session.revokedAt,
		session.createdAt,
	).Scan(
		&sessionID,
	)
	if err != nil {
		f.t.Fatalf(
			"create authentication session: %v",
			err,
		)
	}

	refreshExpiresAt := session.expiresAt.Add(
		-time.Hour,
	)

	if refreshExpiresAt.Before(
		session.createdAt,
	) {
		refreshExpiresAt = session.createdAt.Add(
			time.Hour,
		)
	}

	_, err = f.pool.Exec(
		f.ctx,
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
		session.tokenHash,
		refreshExpiresAt,
		session.createdAt,
	)
	if err != nil {
		f.t.Fatalf(
			"create refresh token: %v",
			err,
		)
	}
}

//go:build integration

package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/database"
	"github.com/jackc/pgx/v5/pgxpool"
)

type sessionRevocationIntegrationFixture struct {
	t                *testing.T
	ctx              context.Context
	pool             *pgxpool.Pool
	identityID       string
	sessionID        string
	now              time.Time
	sessionExpiresAt time.Time
}

func newSessionRevocationIntegrationFixture(
	t *testing.T,
	phoneNumber string,
) *sessionRevocationIntegrationFixture {
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

	_, err = pool.Exec(
		ctx,
		`
			DELETE FROM identities AS i
			USING identity_identifiers AS ii
			WHERE ii.identity_id = i.id
			  AND ii.identifier_type = 'phone'
			  AND ii.normalized_value = $1
		`,
		phoneNumber,
	)
	if err != nil {
		t.Fatalf(
			"clean existing test identity: %v",
			err,
		)
	}

	t.Cleanup(func() {
		_, cleanupErr := pool.Exec(
			context.Background(),
			`
				DELETE FROM identities AS i
				USING identity_identifiers AS ii
				WHERE ii.identity_id = i.id
				  AND ii.identifier_type = 'phone'
				  AND ii.normalized_value = $1
			`,
			phoneNumber,
		)
		if cleanupErr != nil {
			t.Errorf(
				"clean test identity: %v",
				cleanupErr,
			)
		}
	})

	now := time.Now().
		UTC().
		Truncate(time.Microsecond)

	var identityID string

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
		now,
		phoneNumber,
	).Scan(
		&identityID,
	)
	if err != nil {
		t.Fatalf(
			"create test identity: %v",
			err,
		)
	}

	sessionExpiresAt := now.Add(
		30 * 24 * time.Hour,
	)

	var sessionID string

	err = pool.QueryRow(
		ctx,
		`
			INSERT INTO auth_sessions (
				identity_id,
				expires_at,
				created_at,
				updated_at
			)
			VALUES (
				$1::uuid,
				$2,
				$3,
				$3
			)
			RETURNING id::text
		`,
		identityID,
		sessionExpiresAt,
		now,
	).Scan(
		&sessionID,
	)
	if err != nil {
		t.Fatalf(
			"create authentication session: %v",
			err,
		)
	}

	return &sessionRevocationIntegrationFixture{
		t:                t,
		ctx:              ctx,
		pool:             pool,
		identityID:       identityID,
		sessionID:        sessionID,
		now:              now,
		sessionExpiresAt: sessionExpiresAt,
	}
}

func (f *sessionRevocationIntegrationFixture) createRefreshToken(
	tokenHash string,
	expiresAt time.Time,
) {
	f.t.Helper()

	_, err := f.pool.Exec(
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
		f.sessionID,
		tokenHash,
		expiresAt,
		f.now,
	)
	if err != nil {
		f.t.Fatalf(
			"create refresh token: %v",
			err,
		)
	}
}

func (f *sessionRevocationIntegrationFixture) sessionRevokedAt() *time.Time {
	f.t.Helper()

	var revokedAt *time.Time

	err := f.pool.QueryRow(
		f.ctx,
		`
			SELECT revoked_at
			FROM auth_sessions
			WHERE id = $1::uuid
		`,
		f.sessionID,
	).Scan(
		&revokedAt,
	)
	if err != nil {
		f.t.Fatalf(
			"query authentication session: %v",
			err,
		)
	}

	return revokedAt
}

func (f *sessionRevocationIntegrationFixture) refreshTokenCounts() (
	int,
	int,
	int,
) {
	f.t.Helper()

	var (
		total   int
		revoked int
		active  int
	)

	err := f.pool.QueryRow(
		f.ctx,
		`
			SELECT
				COUNT(*),
				COUNT(*) FILTER (
					WHERE revoked_at IS NOT NULL
				),
				COUNT(*) FILTER (
					WHERE revoked_at IS NULL
				)
			FROM refresh_tokens
			WHERE session_id = $1::uuid
		`,
		f.sessionID,
	).Scan(
		&total,
		&revoked,
		&active,
	)
	if err != nil {
		f.t.Fatalf(
			"query session refresh tokens: %v",
			err,
		)
	}

	return total, revoked, active
}

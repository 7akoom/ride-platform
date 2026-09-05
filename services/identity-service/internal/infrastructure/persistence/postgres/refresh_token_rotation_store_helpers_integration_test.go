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

type refreshTokenRotationIntegrationFixture struct {
	t                *testing.T
	ctx              context.Context
	pool             *pgxpool.Pool
	identityID       string
	sessionID        string
	now              time.Time
	sessionExpiresAt time.Time
}

func newRefreshTokenRotationIntegrationFixture(
	t *testing.T,
	phoneNumber string,
) *refreshTokenRotationIntegrationFixture {
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

	return &refreshTokenRotationIntegrationFixture{
		t:                t,
		ctx:              ctx,
		pool:             pool,
		identityID:       identityID,
		sessionID:        sessionID,
		now:              now,
		sessionExpiresAt: sessionExpiresAt,
	}
}

func (f *refreshTokenRotationIntegrationFixture) createRefreshToken(
	tokenHash string,
	expiresAt time.Time,
) string {
	f.t.Helper()

	var tokenID string

	err := f.pool.QueryRow(
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
			RETURNING id::text
		`,
		f.sessionID,
		tokenHash,
		expiresAt,
		f.now,
	).Scan(
		&tokenID,
	)
	if err != nil {
		f.t.Fatalf(
			"create current refresh token: %v",
			err,
		)
	}

	return tokenID
}

func (f *refreshTokenRotationIntegrationFixture) sessionRevokedAt() *time.Time {
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

func (f *refreshTokenRotationIntegrationFixture) activeRefreshTokenCount() int {
	f.t.Helper()

	var count int

	err := f.pool.QueryRow(
		f.ctx,
		`
			SELECT COUNT(*)
			FROM refresh_tokens
			WHERE session_id = $1::uuid
			  AND revoked_at IS NULL
		`,
		f.sessionID,
	).Scan(
		&count,
	)
	if err != nil {
		f.t.Fatalf(
			"count active refresh tokens: %v",
			err,
		)
	}

	return count
}

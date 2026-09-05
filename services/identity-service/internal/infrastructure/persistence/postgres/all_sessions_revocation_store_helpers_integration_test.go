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

type allSessionsRevocationTestFixture struct {
	t          *testing.T
	ctx        context.Context
	pool       *pgxpool.Pool
	identityID string
	now        time.Time
}

type allSessionsRevocationState struct {
	sessionRevokedAt *time.Time
	tokenRevokedAt   *time.Time
}

func newAllSessionsRevocationTestFixture(
	t *testing.T,
	phoneNumber string,
) *allSessionsRevocationTestFixture {
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

	return &allSessionsRevocationTestFixture{
		t:          t,
		ctx:        ctx,
		pool:       pool,
		identityID: identityID,
		now:        now,
	}
}

func (f *allSessionsRevocationTestFixture) createSessionWithRefreshToken(
	createdAt time.Time,
	tokenHash string,
) string {
	f.t.Helper()

	var sessionID string

	err := f.pool.QueryRow(
		f.ctx,
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
		f.identityID,
		createdAt.Add(30*24*time.Hour),
		createdAt,
	).Scan(
		&sessionID,
	)
	if err != nil {
		f.t.Fatalf(
			"create authentication session: %v",
			err,
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
		tokenHash,
		createdAt.Add(29*24*time.Hour),
		createdAt,
	)
	if err != nil {
		f.t.Fatalf(
			"create refresh token: %v",
			err,
		)
	}

	return sessionID
}

func (f *allSessionsRevocationTestFixture) sessionCounts() (
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
			FROM auth_sessions
			WHERE identity_id = $1::uuid
		`,
		f.identityID,
	).Scan(
		&total,
		&revoked,
		&active,
	)
	if err != nil {
		f.t.Fatalf(
			"query authentication sessions: %v",
			err,
		)
	}

	return total, revoked, active
}

func (f *allSessionsRevocationTestFixture) refreshTokenCounts() (
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
					WHERE rt.revoked_at IS NOT NULL
				),
				COUNT(*) FILTER (
					WHERE rt.revoked_at IS NULL
				)
			FROM refresh_tokens AS rt
			INNER JOIN auth_sessions AS s
				ON s.id = rt.session_id
			WHERE s.identity_id = $1::uuid
		`,
		f.identityID,
	).Scan(
		&total,
		&revoked,
		&active,
	)
	if err != nil {
		f.t.Fatalf(
			"query identity refresh tokens: %v",
			err,
		)
	}

	return total, revoked, active
}

func (f *allSessionsRevocationTestFixture) readRevocationState(
	sessionID string,
	tokenHash string,
) allSessionsRevocationState {
	f.t.Helper()

	var state allSessionsRevocationState

	err := f.pool.QueryRow(
		f.ctx,
		`
			SELECT
				s.revoked_at,
				rt.revoked_at
			FROM auth_sessions AS s
			INNER JOIN refresh_tokens AS rt
				ON rt.session_id = s.id
			WHERE s.id = $1::uuid
			  AND rt.token_hash = $2
		`,
		sessionID,
		tokenHash,
	).Scan(
		&state.sessionRevokedAt,
		&state.tokenRevokedAt,
	)
	if err != nil {
		f.t.Fatalf(
			"query revocation state: %v",
			err,
		)
	}

	return state
}

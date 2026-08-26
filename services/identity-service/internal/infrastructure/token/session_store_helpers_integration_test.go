//go:build integration

package token

import (
	"context"
	"os"
	"testing"
	"time"

	databaseinfra "github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/database"
	"github.com/jackc/pgx/v5/pgxpool"
)

type sessionStoreIntegrationFixture struct {
	t           *testing.T
	ctx         context.Context
	pool        *pgxpool.Pool
	phoneNumber string
	identityID  string
}

func newSessionStoreIntegrationFixture(
	t *testing.T,
	phoneNumber string,
) *sessionStoreIntegrationFixture {
	t.Helper()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("DATABASE_URL is required for integration test")
	}

	ctx := context.Background()

	pool, err := databaseinfra.NewPostgresPool(
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

	var identityID string

	err = pool.QueryRow(
		ctx,
		`
			WITH created_identity AS (
				INSERT INTO identities DEFAULT VALUES
				RETURNING id
			)
			INSERT INTO identity_identifiers (
				identity_id,
				identifier_type,
				normalized_value,
				verified_at
			)
			SELECT
				id,
				'phone',
				$1,
				CURRENT_TIMESTAMP
			FROM created_identity
			RETURNING identity_id::text
		`,
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

	return &sessionStoreIntegrationFixture{
		t:           t,
		ctx:         ctx,
		pool:        pool,
		phoneNumber: phoneNumber,
		identityID:  identityID,
	}
}

func (f *sessionStoreIntegrationFixture) generateSessionID() string {
	f.t.Helper()

	var sessionID string

	err := f.pool.QueryRow(
		f.ctx,
		"SELECT gen_random_uuid()::text",
	).Scan(
		&sessionID,
	)
	if err != nil {
		f.t.Fatalf(
			"generate session ID: %v",
			err,
		)
	}

	return sessionID
}

func (f *sessionStoreIntegrationFixture) createOTPChallenge(
	challengeID string,
	codeHash string,
	expiresAt time.Time,
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
				expires_at
			)
			VALUES (
				$1,
				'phone',
				$2,
				'login',
				NULL,
				$3,
				$4
			)
		`,
		challengeID,
		f.phoneNumber,
		codeHash,
		expiresAt,
	)
	if err != nil {
		f.t.Fatalf(
			"create OTP challenge: %v",
			err,
		)
	}

	f.t.Cleanup(func() {
		_, cleanupErr := f.pool.Exec(
			context.Background(),
			"DELETE FROM otp_challenges WHERE id = $1",
			challengeID,
		)
		if cleanupErr != nil {
			f.t.Errorf(
				"clean OTP challenge: %v",
				cleanupErr,
			)
		}
	})
}

func (f *sessionStoreIntegrationFixture) countAuthSessions() int {
	f.t.Helper()

	var count int

	err := f.pool.QueryRow(
		f.ctx,
		`
			SELECT COUNT(*)
			FROM auth_sessions
			WHERE identity_id = $1::uuid
		`,
		f.identityID,
	).Scan(
		&count,
	)
	if err != nil {
		f.t.Fatalf(
			"count auth sessions: %v",
			err,
		)
	}

	return count
}

func (f *sessionStoreIntegrationFixture) countRefreshTokens() int {
	f.t.Helper()

	var count int

	err := f.pool.QueryRow(
		f.ctx,
		`
			SELECT COUNT(*)
			FROM refresh_tokens AS rt
			INNER JOIN auth_sessions AS s
				ON s.id = rt.session_id
			WHERE s.identity_id = $1::uuid
		`,
		f.identityID,
	).Scan(
		&count,
	)
	if err != nil {
		f.t.Fatalf(
			"count refresh tokens: %v",
			err,
		)
	}

	return count
}

func (f *sessionStoreIntegrationFixture) challengeVerifiedAt(
	challengeID string,
) *time.Time {
	f.t.Helper()

	var verifiedAt *time.Time

	err := f.pool.QueryRow(
		f.ctx,
		`
			SELECT verified_at
			FROM otp_challenges
			WHERE id = $1
		`,
		challengeID,
	).Scan(
		&verifiedAt,
	)
	if err != nil {
		f.t.Fatalf(
			"query OTP challenge: %v",
			err,
		)
	}

	return verifiedAt
}

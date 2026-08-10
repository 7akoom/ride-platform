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

func TestAllSessionsRevocationStoreRevokesAllSessionsAndOldTokenCannotRevokeNewSession(
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

	const phoneNumber = "+9647500000052"

	_, err = pool.Exec(
		ctx,
		`
			DELETE FROM identities
			WHERE phone_number = $1
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
				DELETE FROM identities
				WHERE phone_number = $1
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
			INSERT INTO identities (
				phone_number,
				created_at,
				updated_at
			)
			VALUES (
				$1,
				$2,
				$2
			)
			RETURNING id::text
		`,
		phoneNumber,
		now,
	).Scan(
		&identityID,
	)
	if err != nil {
		t.Fatalf(
			"create test identity: %v",
			err,
		)
	}

	var firstSessionID string

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
		now.Add(30*24*time.Hour),
		now,
	).Scan(
		&firstSessionID,
	)
	if err != nil {
		t.Fatalf(
			"create first authentication session: %v",
			err,
		)
	}

	secondSessionCreatedAt := now.Add(
		time.Second,
	)

	var secondSessionID string

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
		secondSessionCreatedAt.Add(
			30*24*time.Hour,
		),
		secondSessionCreatedAt,
	).Scan(
		&secondSessionID,
	)
	if err != nil {
		t.Fatalf(
			"create second authentication session: %v",
			err,
		)
	}

	firstTokenHash := strings.Repeat(
		"f",
		64,
	)

	secondTokenHash := strings.Repeat(
		"1",
		64,
	)

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
		firstSessionID,
		firstTokenHash,
		now.Add(29*24*time.Hour),
		now,
	)
	if err != nil {
		t.Fatalf(
			"create first refresh token: %v",
			err,
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
		secondSessionID,
		secondTokenHash,
		secondSessionCreatedAt.Add(
			29*24*time.Hour,
		),
		secondSessionCreatedAt,
	)
	if err != nil {
		t.Fatalf(
			"create second refresh token: %v",
			err,
		)
	}

	store := NewAllSessionsRevocationStore(
		pool,
	)

	logoutAllAt := now.Add(
		time.Minute,
	)

	err = store.RevokeAllByRefreshTokenHash(
		ctx,
		firstTokenHash,
		logoutAllAt,
	)
	if err != nil {
		t.Fatalf(
			"RevokeAllByRefreshTokenHash() returned an error: %v",
			err,
		)
	}

	var (
		totalSessions   int
		revokedSessions int
		activeSessions  int
	)

	err = pool.QueryRow(
		ctx,
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
		identityID,
	).Scan(
		&totalSessions,
		&revokedSessions,
		&activeSessions,
	)
	if err != nil {
		t.Fatalf(
			"query authentication sessions: %v",
			err,
		)
	}

	if totalSessions != 2 {
		t.Fatalf(
			"session count = %d, expected 2",
			totalSessions,
		)
	}

	if revokedSessions != 2 {
		t.Fatalf(
			"revoked session count = %d, expected 2",
			revokedSessions,
		)
	}

	if activeSessions != 0 {
		t.Fatalf(
			"active session count = %d, expected 0",
			activeSessions,
		)
	}

	var (
		totalRefreshTokens   int
		revokedRefreshTokens int
		activeRefreshTokens  int
	)

	err = pool.QueryRow(
		ctx,
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
		identityID,
	).Scan(
		&totalRefreshTokens,
		&revokedRefreshTokens,
		&activeRefreshTokens,
	)
	if err != nil {
		t.Fatalf(
			"query identity refresh tokens: %v",
			err,
		)
	}

	if totalRefreshTokens != 2 {
		t.Fatalf(
			"refresh token count = %d, expected 2",
			totalRefreshTokens,
		)
	}

	if revokedRefreshTokens != 2 {
		t.Fatalf(
			"revoked refresh token count = %d, expected 2",
			revokedRefreshTokens,
		)
	}

	if activeRefreshTokens != 0 {
		t.Fatalf(
			"active refresh token count = %d, expected 0",
			activeRefreshTokens,
		)
	}

	newSessionCreatedAt := logoutAllAt.Add(
		time.Minute,
	)

	var newSessionID string

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
		newSessionCreatedAt.Add(
			30*24*time.Hour,
		),
		newSessionCreatedAt,
	).Scan(
		&newSessionID,
	)
	if err != nil {
		t.Fatalf(
			"create new authentication session: %v",
			err,
		)
	}

	newTokenHash := strings.Repeat(
		"2",
		64,
	)

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
		newSessionID,
		newTokenHash,
		newSessionCreatedAt.Add(
			29*24*time.Hour,
		),
		newSessionCreatedAt,
	)
	if err != nil {
		t.Fatalf(
			"create new refresh token: %v",
			err,
		)
	}

	err = store.RevokeAllByRefreshTokenHash(
		ctx,
		firstTokenHash,
		newSessionCreatedAt.Add(time.Minute),
	)
	if err != nil {
		t.Fatalf(
			"old refresh token returned an error: %v",
			err,
		)
	}

	var newSessionRevokedAt *time.Time

	err = pool.QueryRow(
		ctx,
		`
			SELECT revoked_at
			FROM auth_sessions
			WHERE id = $1::uuid
		`,
		newSessionID,
	).Scan(
		&newSessionRevokedAt,
	)
	if err != nil {
		t.Fatalf(
			"query new authentication session: %v",
			err,
		)
	}

	if newSessionRevokedAt != nil {
		t.Fatal(
			"old refresh token revoked a newly created session",
		)
	}

	var newTokenRevokedAt *time.Time

	err = pool.QueryRow(
		ctx,
		`
			SELECT revoked_at
			FROM refresh_tokens
			WHERE token_hash = $1
		`,
		newTokenHash,
	).Scan(
		&newTokenRevokedAt,
	)
	if err != nil {
		t.Fatalf(
			"query new refresh token: %v",
			err,
		)
	}

	if newTokenRevokedAt != nil {
		t.Fatal(
			"old refresh token revoked a newly created refresh token",
		)
	}
}
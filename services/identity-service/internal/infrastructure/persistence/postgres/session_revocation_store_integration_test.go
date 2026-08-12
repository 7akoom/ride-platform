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

func TestSessionRevocationStoreRevokesSessionAndAllRefreshTokens(
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

	const phoneNumber = "+9647500000051"

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
			VALUES ($1, $2, $2)
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

	firstTokenHash := strings.Repeat(
		"c",
		64,
	)

	secondTokenHash := strings.Repeat(
		"d",
		64,
	)

	refreshExpiresAt := now.Add(
		29 * 24 * time.Hour,
	)

	for _, tokenHash := range []string{
		firstTokenHash,
		secondTokenHash,
	} {
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
			tokenHash,
			refreshExpiresAt,
			now,
		)
		if err != nil {
			t.Fatalf(
				"create refresh token: %v",
				err,
			)
		}
	}

	store := NewSessionRevocationStore(
		pool,
	)

	target, found, err := store.FindRevocationTargetByRefreshTokenHash(
		ctx,
		firstTokenHash,
	)
	if err != nil {
		t.Fatalf(
			"FindRevocationTargetByRefreshTokenHash() returned an error: %v",
			err,
		)
	}

	if !found {
		t.Fatal(
			"FindRevocationTargetByRefreshTokenHash() did not find known refresh token",
		)
	}

	if target.SessionID != sessionID {
		t.Fatalf(
			"revocation target session ID = %q, expected %q",
			target.SessionID,
			sessionID,
		)
	}

	if !target.SessionExpiresAt.Equal(sessionExpiresAt) {
		t.Fatalf(
			"revocation target expiration = %v, expected %v",
			target.SessionExpiresAt,
			sessionExpiresAt,
		)
	}

	unknownTarget, found, err := store.FindRevocationTargetByRefreshTokenHash(
		ctx,
		strings.Repeat("e", 64),
	)
	if err != nil {
		t.Fatalf(
			"unknown refresh token lookup returned an error: %v",
			err,
		)
	}

	if found {
		t.Fatal(
			"unknown refresh token lookup unexpectedly found a session",
		)
	}

	if unknownTarget.SessionID != "" ||
		!unknownTarget.SessionExpiresAt.IsZero() {
		t.Fatalf(
			"unknown refresh token target = %+v, expected zero value",
			unknownTarget,
		)
	}

	revokedAt := now.Add(
		time.Minute,
	)

	err = store.RevokeByRefreshTokenHash(
		ctx,
		firstTokenHash,
		revokedAt,
	)
	if err != nil {
		t.Fatalf(
			"RevokeByRefreshTokenHash() returned an error: %v",
			err,
		)
	}

	var sessionRevokedAt *time.Time

	err = pool.QueryRow(
		ctx,
		`
			SELECT revoked_at
			FROM auth_sessions
			WHERE id = $1::uuid
		`,
		sessionID,
	).Scan(
		&sessionRevokedAt,
	)
	if err != nil {
		t.Fatalf(
			"query authentication session: %v",
			err,
		)
	}

	if sessionRevokedAt == nil {
		t.Fatal(
			"authentication session was not revoked",
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
					WHERE revoked_at IS NOT NULL
				),
				COUNT(*) FILTER (
					WHERE revoked_at IS NULL
				)
			FROM refresh_tokens
			WHERE session_id = $1::uuid
		`,
		sessionID,
	).Scan(
		&totalRefreshTokens,
		&revokedRefreshTokens,
		&activeRefreshTokens,
	)
	if err != nil {
		t.Fatalf(
			"query session refresh tokens: %v",
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

	err = store.RevokeByRefreshTokenHash(
		ctx,
		firstTokenHash,
		revokedAt.Add(time.Second),
	)
	if err != nil {
		t.Fatalf(
			"second logout returned an error: %v",
			err,
		)
	}

	err = store.RevokeByRefreshTokenHash(
		ctx,
		strings.Repeat("e", 64),
		revokedAt.Add(2*time.Second),
	)
	if err != nil {
		t.Fatalf(
			"unknown refresh token returned an error: %v",
			err,
		)
	}
}

func TestSessionRevocationStoreFindsSessionAccessState(
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
			VALUES ($1, $2, $2)
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

	store := NewSessionRevocationStore(
		pool,
	)

	activeState, found, err :=
		store.FindSessionAccessState(
			ctx,
			sessionID,
		)
	if err != nil {
		t.Fatalf(
			"FindSessionAccessState() for active session returned an error: %v",
			err,
		)
	}

	if !found {
		t.Fatal(
			"FindSessionAccessState() did not find active session",
		)
	}

	if activeState.Revoked {
		t.Fatal(
			"active session was unexpectedly reported as revoked",
		)
	}

	if !activeState.SessionExpiresAt.Equal(
		sessionExpiresAt,
	) {
		t.Fatalf(
			"active session expiration = %v, expected %v",
			activeState.SessionExpiresAt,
			sessionExpiresAt,
		)
	}

	revokedAt := now.Add(
		time.Minute,
	)

	_, err = pool.Exec(
		ctx,
		`
			UPDATE auth_sessions
			SET
				revoked_at = $2,
				updated_at = $2
			WHERE id = $1::uuid
		`,
		sessionID,
		revokedAt,
	)
	if err != nil {
		t.Fatalf(
			"revoke authentication session: %v",
			err,
		)
	}

	revokedState, found, err :=
		store.FindSessionAccessState(
			ctx,
			sessionID,
		)
	if err != nil {
		t.Fatalf(
			"FindSessionAccessState() for revoked session returned an error: %v",
			err,
		)
	}

	if !found {
		t.Fatal(
			"FindSessionAccessState() did not find revoked session",
		)
	}

	if !revokedState.Revoked {
		t.Fatal(
			"revoked session was unexpectedly reported as active",
		)
	}

	if !revokedState.SessionExpiresAt.Equal(
		sessionExpiresAt,
	) {
		t.Fatalf(
			"revoked session expiration = %v, expected %v",
			revokedState.SessionExpiresAt,
			sessionExpiresAt,
		)
	}

	const missingSessionID = "ffffffff-ffff-4fff-8fff-ffffffffffff"

	missingState, found, err :=
		store.FindSessionAccessState(
			ctx,
			missingSessionID,
		)
	if err != nil {
		t.Fatalf(
			"FindSessionAccessState() for missing session returned an error: %v",
			err,
		)
	}

	if found {
		t.Fatal(
			"FindSessionAccessState() unexpectedly found missing session",
		)
	}

	if missingState.Revoked ||
		!missingState.SessionExpiresAt.IsZero() {
		t.Fatalf(
			"missing session state = %+v, expected zero value",
			missingState,
		)
	}
}

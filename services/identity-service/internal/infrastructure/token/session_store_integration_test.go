//go:build integration

package token

import (
	"context"
	"os"
	"testing"
	"time"

	databaseinfra "github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/database"
)

func TestSessionStoreCreateIsAtomic(t *testing.T) {
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
		t.Fatalf("connect to PostgreSQL: %v", err)
	}

	t.Cleanup(pool.Close)

	const phoneNumber = "+9647500000003"

	_, err = pool.Exec(
		ctx,
		"DELETE FROM identities WHERE phone_number = $1",
		phoneNumber,
	)
	if err != nil {
		t.Fatalf("clean existing test identity: %v", err)
	}

	t.Cleanup(func() {
		_, cleanupErr := pool.Exec(
			context.Background(),
			"DELETE FROM identities WHERE phone_number = $1",
			phoneNumber,
		)
		if cleanupErr != nil {
			t.Errorf("clean test identity: %v", cleanupErr)
		}
	})

	var identityID string

	err = pool.QueryRow(
		ctx,
		`
			INSERT INTO identities (
				phone_number
			)
			VALUES ($1)
			RETURNING id::text
		`,
		phoneNumber,
	).Scan(&identityID)
	if err != nil {
		t.Fatalf("create test identity: %v", err)
	}

	var firstSessionID string

	if err := pool.QueryRow(
		ctx,
		"SELECT gen_random_uuid()::text",
	).Scan(&firstSessionID); err != nil {
		t.Fatalf("generate first session ID: %v", err)
	}

	var secondSessionID string

	if err := pool.QueryRow(
		ctx,
		"SELECT gen_random_uuid()::text",
	).Scan(&secondSessionID); err != nil {
		t.Fatalf("generate second session ID: %v", err)
	}

	store := NewSessionStore(pool)

	refreshToken := "rt_session-store-integration-test"
	refreshTokenHash := HashRefreshToken(refreshToken)

	now := time.Now().UTC()

	sessionExpiresAt := now.Add(30 * 24 * time.Hour)
	refreshTokenExpiresAt := now.Add(29 * 24 * time.Hour)

	issuedSession, err := store.Create(
		ctx,
		firstSessionID,
		identityID,
		sessionExpiresAt,
		refreshTokenHash,
		refreshTokenExpiresAt,
	)
	if err != nil {
		t.Fatalf("Create() returned an error: %v", err)
	}

	if issuedSession.SessionID != firstSessionID {
		t.Fatalf(
			"Create() returned session ID %q, expected %q",
			issuedSession.SessionID,
			firstSessionID,
		)
	}

	if issuedSession.RefreshTokenID == "" {
		t.Fatal("Create() returned an empty refresh token ID")
	}

	var storedIdentityID string
	var storedTokenHash string

	err = pool.QueryRow(
		ctx,
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

	if storedIdentityID != identityID {
		t.Fatalf(
			"stored identity ID is %q, expected %q",
			storedIdentityID,
			identityID,
		)
	}

	if storedTokenHash != refreshTokenHash {
		t.Fatal("stored refresh token hash does not match expected hash")
	}

	_, err = store.Create(
		ctx,
		secondSessionID,
		identityID,
		sessionExpiresAt,
		refreshTokenHash,
		refreshTokenExpiresAt,
	)

	if err == nil {
		t.Fatal(
			"Create() accepted a duplicate refresh token hash",
		)
	}

	var sessionCount int

	err = pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM auth_sessions
			WHERE identity_id = $1::uuid
		`,
		identityID,
	).Scan(&sessionCount)
	if err != nil {
		t.Fatalf("count auth sessions: %v", err)
	}

	if sessionCount != 1 {
		t.Fatalf(
			"database contains %d sessions, expected 1 after failed transaction",
			sessionCount,
		)
	}

	var refreshTokenCount int

	err = pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM refresh_tokens AS rt
			INNER JOIN auth_sessions AS s
				ON s.id = rt.session_id
			WHERE s.identity_id = $1::uuid
		`,
		identityID,
	).Scan(&refreshTokenCount)
	if err != nil {
		t.Fatalf("count refresh tokens: %v", err)
	}

	if refreshTokenCount != 1 {
		t.Fatalf(
			"database contains %d refresh tokens, expected 1",
			refreshTokenCount,
		)
	}
}
//go:build integration

package token

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
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

	firstChallengeID := "otp-" + firstSessionID
	secondChallengeID := "otp-" + secondSessionID

	_, err = pool.Exec(
		ctx,
		`
			INSERT INTO otp_challenges (
				id,
				phone_number,
				code_hash,
				expires_at
			)
			VALUES
				($1, $2, $3, $4),
				($5, $2, $6, $4)
		`,
		firstChallengeID,
		phoneNumber,
		strings.Repeat("a", 64),
		now.Add(5*time.Minute),
		secondChallengeID,
		strings.Repeat("b", 64),
	)
	if err != nil {
		t.Fatalf(
			"create OTP challenges: %v",
			err,
		)
	}

	verifiedAt := time.Now().UTC()

	t.Cleanup(func() {
		_, cleanupErr := pool.Exec(
			context.Background(),
			`
				DELETE FROM otp_challenges
				WHERE id IN ($1, $2)
			`,
			firstChallengeID,
			secondChallengeID,
		)
		if cleanupErr != nil {
			t.Errorf(
				"clean OTP challenges: %v",
				cleanupErr,
			)
		}
	})

	sessionExpiresAt := now.Add(30 * 24 * time.Hour)
	refreshTokenExpiresAt := now.Add(29 * 24 * time.Hour)

	issuedSession, err := store.Create(
		ctx,
		SessionCreationInput{
			ChallengeID:           firstChallengeID,
			VerifiedAt:            verifiedAt,
			SessionID:             firstSessionID,
			IdentityID:            identityID,
			SessionExpiresAt:      sessionExpiresAt,
			RefreshTokenHash:      refreshTokenHash,
			RefreshTokenExpiresAt: refreshTokenExpiresAt,
		},
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
		SessionCreationInput{
			ChallengeID:           secondChallengeID,
			VerifiedAt:            verifiedAt,
			SessionID:             secondSessionID,
			IdentityID:            identityID,
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

	var rolledBackVerifiedAt *time.Time

	err = pool.QueryRow(
		ctx,
		`
			SELECT verified_at
			FROM otp_challenges
			WHERE id = $1
		`,
		secondChallengeID,
	).Scan(&rolledBackVerifiedAt)
	if err != nil {
		t.Fatalf(
			"query second OTP challenge after failed transaction: %v",
			err,
		)
	}

	if rolledBackVerifiedAt != nil {
		t.Fatal(
			"second OTP challenge remained verified after failed session transaction",
		)
	}
}

func TestSessionStoreCreateRejectsInvalidRefreshTokenHash(
	t *testing.T,
) {
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

	const phoneNumber = "+9647500000004"

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

	var sessionID string

	err = pool.QueryRow(
		ctx,
		"SELECT gen_random_uuid()::text",
	).Scan(&sessionID)
	if err != nil {
		t.Fatalf("generate session ID: %v", err)
	}

	store := NewSessionStore(pool)

	now := time.Now().UTC()

	challengeID := "otp-" + sessionID

	_, err = pool.Exec(
		ctx,
		`
			INSERT INTO otp_challenges (
				id,
				phone_number,
				code_hash,
				expires_at
			)
			VALUES ($1, $2, $3, $4)
		`,
		challengeID,
		phoneNumber,
		strings.Repeat("c", 64),
		now.Add(5*time.Minute),
	)
	if err != nil {
		t.Fatalf(
			"create OTP challenge: %v",
			err,
		)
	}

	verifiedAt := time.Now().UTC()

	t.Cleanup(func() {
		_, cleanupErr := pool.Exec(
			context.Background(),
			"DELETE FROM otp_challenges WHERE id = $1",
			challengeID,
		)
		if cleanupErr != nil {
			t.Errorf(
				"clean OTP challenge: %v",
				cleanupErr,
			)
		}
	})

	invalidRefreshTokenHash := strings.Repeat("A", 64)

	_, err = store.Create(
		ctx,
		SessionCreationInput{
			ChallengeID:           challengeID,
			VerifiedAt:            verifiedAt,
			SessionID:             sessionID,
			IdentityID:            identityID,
			SessionExpiresAt:      now.Add(24 * time.Hour),
			RefreshTokenHash:      invalidRefreshTokenHash,
			RefreshTokenExpiresAt: now.Add(23 * time.Hour),
		},
	)

	if err == nil {
		t.Fatal(
			"Create() accepted an invalid refresh token hash",
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

	if sessionCount != 0 {
		t.Fatalf(
			"database contains %d sessions, expected 0 after rejected refresh token hash",
			sessionCount,
		)
	}

	var rolledBackVerifiedAt *time.Time

	err = pool.QueryRow(
		ctx,
		`
			SELECT verified_at
			FROM otp_challenges
			WHERE id = $1
		`,
		challengeID,
	).Scan(&rolledBackVerifiedAt)
	if err != nil {
		t.Fatalf(
			"query OTP challenge after rejected refresh token hash: %v",
			err,
		)
	}

	if rolledBackVerifiedAt != nil {
		t.Fatal(
			"OTP challenge remained verified after session transaction rollback",
		)
	}
}

func TestSessionStoreCreateAllowsOnlyOneConcurrentChallengeClaim(
	t *testing.T,
) {
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

	const phoneNumber = "+9647500000006"

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
	var secondSessionID string

	err = pool.QueryRow(
		ctx,
		`
			SELECT
				gen_random_uuid()::text,
				gen_random_uuid()::text
		`,
	).Scan(
		&firstSessionID,
		&secondSessionID,
	)
	if err != nil {
		t.Fatalf(
			"generate session IDs: %v",
			err,
		)
	}

	challengeID := "otp-concurrency-" + identityID
	now := time.Now().UTC()

	_, err = pool.Exec(
		ctx,
		`
			INSERT INTO otp_challenges (
				id,
				phone_number,
				code_hash,
				expires_at
			)
			VALUES ($1, $2, $3, $4)
		`,
		challengeID,
		phoneNumber,
		strings.Repeat("d", 64),
		now.Add(5*time.Minute),
	)
	if err != nil {
		t.Fatalf(
			"create OTP challenge: %v",
			err,
		)
	}

	t.Cleanup(func() {
		_, cleanupErr := pool.Exec(
			context.Background(),
			"DELETE FROM otp_challenges WHERE id = $1",
			challengeID,
		)
		if cleanupErr != nil {
			t.Errorf(
				"clean OTP challenge: %v",
				cleanupErr,
			)
		}
	})

	verifiedAt := time.Now().UTC()

	store := NewSessionStore(pool)

	sessionExpiresAt := verifiedAt.Add(
		30 * 24 * time.Hour,
	)
	refreshTokenExpiresAt := verifiedAt.Add(
		29 * 24 * time.Hour,
	)

	firstInput := SessionCreationInput{
		ChallengeID:      challengeID,
		VerifiedAt:       verifiedAt,
		SessionID:        firstSessionID,
		IdentityID:       identityID,
		SessionExpiresAt: sessionExpiresAt,
		RefreshTokenHash: HashRefreshToken(
			"rt_concurrent_first",
		),
		RefreshTokenExpiresAt: refreshTokenExpiresAt,
	}

	secondInput := SessionCreationInput{
		ChallengeID:      challengeID,
		VerifiedAt:       verifiedAt,
		SessionID:        secondSessionID,
		IdentityID:       identityID,
		SessionExpiresAt: sessionExpiresAt,
		RefreshTokenHash: HashRefreshToken(
			"rt_concurrent_second",
		),
		RefreshTokenExpiresAt: refreshTokenExpiresAt,
	}

	start := make(chan struct{})
	results := make(chan error, 2)

	runCreate := func(input SessionCreationInput) {
		<-start

		_, createErr := store.Create(
			ctx,
			input,
		)

		results <- createErr
	}

	go runCreate(firstInput)
	go runCreate(secondInput)

	close(start)

	firstErr := <-results
	secondErr := <-results

	successCount := 0
	challengeUsedCount := 0

	for _, createErr := range []error{
		firstErr,
		secondErr,
	} {
		switch {
		case createErr == nil:
			successCount++

		case errors.Is(
			createErr,
			auth.ErrChallengeUsed,
		):
			challengeUsedCount++

		default:
			t.Fatalf(
				"concurrent Create() returned unexpected error: %v",
				createErr,
			)
		}
	}

	if successCount != 1 {
		t.Fatalf(
			"successful concurrent Create() calls = %d, expected 1",
			successCount,
		)
	}

	if challengeUsedCount != 1 {
		t.Fatalf(
			"ErrChallengeUsed results = %d, expected 1",
			challengeUsedCount,
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
		t.Fatalf(
			"count auth sessions: %v",
			err,
		)
	}

	if sessionCount != 1 {
		t.Fatalf(
			"database contains %d sessions, expected 1",
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
		t.Fatalf(
			"count refresh tokens: %v",
			err,
		)
	}

	if refreshTokenCount != 1 {
		t.Fatalf(
			"database contains %d refresh tokens, expected 1",
			refreshTokenCount,
		)
	}

	var storedVerifiedAt *time.Time

	err = pool.QueryRow(
		ctx,
		`
			SELECT verified_at
			FROM otp_challenges
			WHERE id = $1
		`,
		challengeID,
	).Scan(&storedVerifiedAt)
	if err != nil {
		t.Fatalf(
			"query OTP challenge: %v",
			err,
		)
	}

	if storedVerifiedAt == nil {
		t.Fatal(
			"OTP challenge was not marked verified",
		)
	}

	verificationTimeDifference :=
		storedVerifiedAt.Sub(verifiedAt)

	if verificationTimeDifference < 0 {
		verificationTimeDifference =
			-verificationTimeDifference
	}

	if verificationTimeDifference > time.Microsecond {
		t.Fatalf(
			"OTP challenge verification time = %v, expected approximately %v",
			*storedVerifiedAt,
			verifiedAt,
		)
	}
}

//go:build integration

package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	databaseinfra "github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/database"
)

func TestChallengeRepositoryCreateFindAndConsumeOnce(t *testing.T) {
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

	repository := NewChallengeRepository(pool)

	const challengeID = "otp_ch_integration_single_use"
	const phoneNumber = "+9647500000001"

	_, err = pool.Exec(
		ctx,
		"DELETE FROM otp_challenges WHERE id = $1",
		challengeID,
	)
	if err != nil {
		t.Fatalf("clean existing test challenge: %v", err)
	}

	t.Cleanup(func() {
		_, cleanupErr := pool.Exec(
			context.Background(),
			"DELETE FROM otp_challenges WHERE id = $1",
			challengeID,
		)
		if cleanupErr != nil {
			t.Errorf("clean test challenge: %v", cleanupErr)
		}
	})

	now := time.Now().UTC()

	challenge := auth.OTPChallenge{
		ID:          challengeID,
		PhoneNumber: phoneNumber,
		CodeHash:    "integration-test-code-hash",
		ExpiresAt:   now.Add(5 * time.Minute),
	}

	if err := repository.Create(ctx, challenge); err != nil {
		t.Fatalf("Create() returned an error: %v", err)
	}

	storedChallenge, err := repository.FindByID(
		ctx,
		challengeID,
	)
	if err != nil {
		t.Fatalf("FindByID() returned an error: %v", err)
	}

	if storedChallenge.ID != challenge.ID {
		t.Fatalf(
			"FindByID() returned ID %q, expected %q",
			storedChallenge.ID,
			challenge.ID,
		)
	}

	if storedChallenge.PhoneNumber != challenge.PhoneNumber {
		t.Fatalf(
			"FindByID() returned phone number %q, expected %q",
			storedChallenge.PhoneNumber,
			challenge.PhoneNumber,
		)
	}

	if storedChallenge.CodeHash != challenge.CodeHash {
		t.Fatalf(
			"FindByID() returned unexpected code hash",
		)
	}

	if storedChallenge.VerifiedAt != nil {
		t.Fatal("new challenge is already marked as verified")
	}

	verifiedAt := time.Now().UTC()

	if err := repository.MarkVerified(
		ctx,
		challengeID,
		verifiedAt,
	); err != nil {
		t.Fatalf("first MarkVerified() returned an error: %v", err)
	}

	err = repository.MarkVerified(
		ctx,
		challengeID,
		verifiedAt.Add(time.Second),
	)

	if !errors.Is(err, auth.ErrChallengeUsed) {
		t.Fatalf(
			"second MarkVerified() returned %v, expected ErrChallengeUsed",
			err,
		)
	}

	consumedChallenge, err := repository.FindByID(
		ctx,
		challengeID,
	)
	if err != nil {
		t.Fatalf(
			"FindByID() after verification returned an error: %v",
			err,
		)
	}

	if consumedChallenge.VerifiedAt == nil {
		t.Fatal("consumed challenge has nil VerifiedAt")
	}
}

func TestChallengeRepositoryKeepsOnlyLatestChallengeActiveConcurrently(
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

	repository := NewChallengeRepository(pool)

	const firstChallengeID = "otp_ch_concurrent_latest_first"
	const secondChallengeID = "otp_ch_concurrent_latest_second"
	const phoneNumber = "+9647500000099"

	_, err = pool.Exec(
		ctx,
		`
			DELETE FROM otp_challenges
			WHERE phone_number = $1
		`,
		phoneNumber,
	)
	if err != nil {
		t.Fatalf(
			"clean existing concurrent test challenges: %v",
			err,
		)
	}

	t.Cleanup(func() {
		_, cleanupErr := pool.Exec(
			context.Background(),
			`
				DELETE FROM otp_challenges
				WHERE phone_number = $1
			`,
			phoneNumber,
		)
		if cleanupErr != nil {
			t.Errorf(
				"clean concurrent test challenges: %v",
				cleanupErr,
			)
		}
	})

	now := time.Now().UTC()

	firstChallenge := auth.OTPChallenge{
		ID:          firstChallengeID,
		PhoneNumber: phoneNumber,
		CodeHash:    "first-concurrent-code-hash",
		ExpiresAt:   now.Add(5 * time.Minute),
	}

	secondChallenge := auth.OTPChallenge{
		ID:          secondChallengeID,
		PhoneNumber: phoneNumber,
		CodeHash:    "second-concurrent-code-hash",
		ExpiresAt:   now.Add(5 * time.Minute),
	}

	start := make(chan struct{})
	result := make(chan error, 2)

	go func() {
		<-start

		result <- repository.Create(
			ctx,
			firstChallenge,
		)
	}()

	go func() {
		<-start

		result <- repository.Create(
			ctx,
			secondChallenge,
		)
	}()

	close(start)

	for i := 0; i < 2; i++ {
		if createErr := <-result; createErr != nil {
			t.Fatalf(
				"concurrent Create() returned an error: %v",
				createErr,
			)
		}
	}

	var totalChallenges int
	var activeChallenges int
	var cancelledChallenges int

	err = pool.QueryRow(
		ctx,
		`
			SELECT
				COUNT(*),
				COUNT(*) FILTER (
					WHERE verified_at IS NULL
					AND cancelled_at IS NULL
					AND expires_at > CURRENT_TIMESTAMP
				),
				COUNT(*) FILTER (
					WHERE cancelled_at IS NOT NULL
				)
			FROM otp_challenges
			WHERE id IN ($1, $2)
		`,
		firstChallengeID,
		secondChallengeID,
	).Scan(
		&totalChallenges,
		&activeChallenges,
		&cancelledChallenges,
	)
	if err != nil {
		t.Fatalf(
			"query concurrent OTP challenge state: %v",
			err,
		)
	}

	if totalChallenges != 2 {
		t.Fatalf(
			"total challenges = %d, expected 2",
			totalChallenges,
		)
	}

	if activeChallenges != 1 {
		t.Fatalf(
			"active challenges = %d, expected 1",
			activeChallenges,
		)
	}

	if cancelledChallenges != 1 {
		t.Fatalf(
			"cancelled challenges = %d, expected 1",
			cancelledChallenges,
		)
	}
}

func TestChallengeRepositoryUsesCancellationTimeAfterWaitingForPhoneLock(
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

	repository := NewChallengeRepository(pool)

	const existingChallengeID = "otp_ch_lock_wait_existing"
	const newChallengeID = "otp_ch_lock_wait_new"
	const phoneNumber = "+9647500000098"

	_, err = pool.Exec(
		ctx,
		`
			DELETE FROM otp_challenges
			WHERE phone_number = $1
		`,
		phoneNumber,
	)
	if err != nil {
		t.Fatalf(
			"clean existing lock-wait test challenges: %v",
			err,
		)
	}

	t.Cleanup(func() {
		_, cleanupErr := pool.Exec(
			context.Background(),
			`
				DELETE FROM otp_challenges
				WHERE phone_number = $1
			`,
			phoneNumber,
		)
		if cleanupErr != nil {
			t.Errorf(
				"clean lock-wait test challenges: %v",
				cleanupErr,
			)
		}
	})

	lockTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf(
			"begin advisory lock transaction: %v",
			err,
		)
	}

	lockReleased := false

	t.Cleanup(func() {
		if !lockReleased {
			_ = lockTx.Rollback(
				context.Background(),
			)
		}
	})

	_, err = lockTx.Exec(
		ctx,
		`
			SELECT pg_advisory_xact_lock(
				hashtextextended($1, 0)
			)
		`,
		phoneNumber,
	)
	if err != nil {
		t.Fatalf(
			"acquire advisory phone lock: %v",
			err,
		)
	}

	createResult := make(chan error, 1)

	newChallenge := auth.OTPChallenge{
		ID:          newChallengeID,
		PhoneNumber: phoneNumber,
		CodeHash:    "new-lock-wait-code-hash",
		ExpiresAt:   time.Now().UTC().Add(5 * time.Minute),
	}

	go func() {
		createResult <- repository.Create(
			context.Background(),
			newChallenge,
		)
	}()

	time.Sleep(100 * time.Millisecond)

	_, err = pool.Exec(
		ctx,
		`
			INSERT INTO otp_challenges (
				id,
				phone_number,
				code_hash,
				expires_at
			)
			VALUES (
				$1,
				$2,
				$3,
				$4
			)
		`,
		existingChallengeID,
		phoneNumber,
		"existing-lock-wait-code-hash",
		time.Now().UTC().Add(5*time.Minute),
	)
	if err != nil {
		t.Fatalf(
			"insert challenge while Create() waits for lock: %v",
			err,
		)
	}

	if err := lockTx.Commit(ctx); err != nil {
		t.Fatalf(
			"release advisory phone lock: %v",
			err,
		)
	}

	lockReleased = true

	select {
	case createErr := <-createResult:
		if createErr != nil {
			t.Fatalf(
				"Create() after waiting for lock returned an error: %v",
				createErr,
			)
		}

	case <-time.After(5 * time.Second):
		t.Fatal(
			"Create() did not finish after advisory lock was released",
		)
	}

	existingChallenge, err := repository.FindByID(
		ctx,
		existingChallengeID,
	)
	if err != nil {
		t.Fatalf(
			"FindByID() for existing challenge returned an error: %v",
			err,
		)
	}

	if existingChallenge.CancelledAt == nil {
		t.Fatal(
			"existing challenge remained active after newer challenge was created",
		)
	}

	newStoredChallenge, err := repository.FindByID(
		ctx,
		newChallengeID,
	)
	if err != nil {
		t.Fatalf(
			"FindByID() for new challenge returned an error: %v",
			err,
		)
	}

	if newStoredChallenge.CancelledAt != nil {
		t.Fatal(
			"new challenge was unexpectedly cancelled",
		)
	}
}

//go:build integration

package postgres

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	"github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/database"
)

func TestChallengeRepositoryConcurrentFailedAttemptsNeverExceedMaximum(
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

	repository := NewChallengeRepository(
		pool,
	)

	now := time.Now().
		UTC().
		Truncate(time.Second)

	challenge := auth.OTPChallenge{
		ID: "otp_ch_attempt_concurrency_integration_test",
		Identifier: auth.Identifier{
			Type:  auth.IdentifierTypePhone,
			Value: "+9647500000059",
		},
		Purpose:   auth.OTPPurposeLogin,
		CodeHash:  "integration-test-code-hash",
		ExpiresAt: now.Add(10 * time.Minute),
	}

	_, err = pool.Exec(
		ctx,
		"DELETE FROM otp_challenges WHERE id = $1",
		challenge.ID,
	)
	if err != nil {
		t.Fatalf(
			"clean existing OTP challenge: %v",
			err,
		)
	}

	t.Cleanup(func() {
		_, cleanupErr := pool.Exec(
			context.Background(),
			"DELETE FROM otp_challenges WHERE id = $1",
			challenge.ID,
		)
		if cleanupErr != nil {
			t.Errorf(
				"clean OTP challenge: %v",
				cleanupErr,
			)
		}
	})

	if err := repository.Create(
		ctx,
		challenge,
	); err != nil {
		t.Fatalf(
			"create OTP challenge: %v",
			err,
		)
	}

	const concurrentAttempts = 32

	start := make(chan struct{})

	results := make(
		chan error,
		concurrentAttempts,
	)

	var waitGroup sync.WaitGroup

	for attempt := 0; attempt < concurrentAttempts; attempt++ {
		waitGroup.Add(1)

		go func() {
			defer waitGroup.Done()

			<-start

			results <- repository.RecordFailedAttempt(
				ctx,
				challenge.ID,
				now.Add(time.Second),
			)
		}()
	}

	close(start)

	waitGroup.Wait()
	close(results)

	successCount := 0
	attemptsExceededCount := 0

	for resultErr := range results {
		switch {
		case resultErr == nil:
			successCount++

		case errors.Is(
			resultErr,
			auth.ErrChallengeAttemptsExceeded,
		):
			attemptsExceededCount++

		default:
			t.Fatalf(
				"concurrent RecordFailedAttempt() returned unexpected error: %v",
				resultErr,
			)
		}
	}

	storedChallenge, err :=
		repository.FindByID(
			ctx,
			challenge.ID,
		)
	if err != nil {
		t.Fatalf(
			"find OTP challenge after concurrent attempts: %v",
			err,
		)
	}

	if storedChallenge.MaxAttempts != 5 {
		t.Fatalf(
			"maximum attempts = %d, expected 5",
			storedChallenge.MaxAttempts,
		)
	}

	if storedChallenge.FailedAttempts !=
		storedChallenge.MaxAttempts {
		t.Fatalf(
			"failed attempts = %d, expected maximum %d",
			storedChallenge.FailedAttempts,
			storedChallenge.MaxAttempts,
		)
	}

	expectedSuccessfulCalls :=
		int(storedChallenge.MaxAttempts) - 1

	if successCount != expectedSuccessfulCalls {
		t.Fatalf(
			"successful failed-attempt updates = %d, expected %d",
			successCount,
			expectedSuccessfulCalls,
		)
	}

	expectedExceededCalls :=
		concurrentAttempts -
			expectedSuccessfulCalls

	if attemptsExceededCount != expectedExceededCalls {
		t.Fatalf(
			"attempts-exceeded results = %d, expected %d",
			attemptsExceededCount,
			expectedExceededCalls,
		)
	}

	err = repository.RecordFailedAttempt(
		ctx,
		challenge.ID,
		now.Add(2*time.Second),
	)

	if !errors.Is(
		err,
		auth.ErrChallengeAttemptsExceeded,
	) {
		t.Fatalf(
			"additional failed attempt returned %v, expected %v",
			err,
			auth.ErrChallengeAttemptsExceeded,
		)
	}

	storedChallenge, err =
		repository.FindByID(
			ctx,
			challenge.ID,
		)
	if err != nil {
		t.Fatalf(
			"find OTP challenge after additional attempt: %v",
			err,
		)
	}

	if storedChallenge.FailedAttempts !=
		storedChallenge.MaxAttempts {
		t.Fatalf(
			"failed attempts changed after challenge lock: %d, expected %d",
			storedChallenge.FailedAttempts,
			storedChallenge.MaxAttempts,
		)
	}
}

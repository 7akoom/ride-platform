//go:build integration

package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	"github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/database"
)

func TestChallengeRepositoryLocksChallengeAfterMaximumFailedAttempts(
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
		t.Fatalf("connect to PostgreSQL: %v", err)
	}

	t.Cleanup(pool.Close)

	repository := NewChallengeRepository(pool)

	now := time.Now().UTC()

	challenge := auth.OTPChallenge{
		ID: "otp_ch_attempt_limit_integration_test",
		Identifier: auth.Identifier{
			Type:  auth.IdentifierTypePhone,
			Value: "+9647500000010",
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

	for attempt := int16(1); attempt <= 4; attempt++ {
		attemptedAt := now.Add(
			time.Duration(attempt) * time.Second,
		)

		err := repository.RecordFailedAttempt(
			ctx,
			challenge.ID,
			attemptedAt,
		)
		if err != nil {
			t.Fatalf(
				"RecordFailedAttempt() attempt %d returned error: %v",
				attempt,
				err,
			)
		}

		storedChallenge, err := repository.FindByID(
			ctx,
			challenge.ID,
		)
		if err != nil {
			t.Fatalf(
				"FindByID() after attempt %d returned error: %v",
				attempt,
				err,
			)
		}

		if storedChallenge.FailedAttempts != attempt {
			t.Fatalf(
				"failed attempts is %d after attempt %d",
				storedChallenge.FailedAttempts,
				attempt,
			)
		}

		if storedChallenge.MaxAttempts != 5 {
			t.Fatalf(
				"max attempts is %d, expected 5",
				storedChallenge.MaxAttempts,
			)
		}
	}

	err = repository.RecordFailedAttempt(
		ctx,
		challenge.ID,
		now.Add(5*time.Second),
	)

	if !errors.Is(
		err,
		auth.ErrChallengeAttemptsExceeded,
	) {
		t.Fatalf(
			"fifth failed attempt returned %v, expected %v",
			err,
			auth.ErrChallengeAttemptsExceeded,
		)
	}

	storedChallenge, err := repository.FindByID(
		ctx,
		challenge.ID,
	)
	if err != nil {
		t.Fatalf(
			"FindByID() after fifth attempt returned error: %v",
			err,
		)
	}

	if storedChallenge.FailedAttempts != 5 {
		t.Fatalf(
			"failed attempts is %d, expected 5",
			storedChallenge.FailedAttempts,
		)
	}

	err = repository.RecordFailedAttempt(
		ctx,
		challenge.ID,
		now.Add(6*time.Second),
	)

	if !errors.Is(
		err,
		auth.ErrChallengeAttemptsExceeded,
	) {
		t.Fatalf(
			"attempt after lock returned %v, expected %v",
			err,
			auth.ErrChallengeAttemptsExceeded,
		)
	}

	storedChallenge, err = repository.FindByID(
		ctx,
		challenge.ID,
	)
	if err != nil {
		t.Fatalf(
			"FindByID() after locked attempt returned error: %v",
			err,
		)
	}

	if storedChallenge.FailedAttempts != 5 {
		t.Fatalf(
			"locked challenge increased to %d attempts, expected 5",
			storedChallenge.FailedAttempts,
		)
	}

	err = repository.MarkVerified(
		ctx,
		challenge.ID,
		now.Add(7*time.Second),
	)

	if !errors.Is(
		err,
		auth.ErrChallengeAttemptsExceeded,
	) {
		t.Fatalf(
			"MarkVerified() returned %v, expected %v",
			err,
			auth.ErrChallengeAttemptsExceeded,
		)
	}
}

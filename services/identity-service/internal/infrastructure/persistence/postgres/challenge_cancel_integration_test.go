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

func TestChallengeRepositoryCancelMakesChallengeUnusable(
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
		ID: "otp_ch_cancel_integration_test",
		Identifier: auth.Identifier{
			Type:  auth.IdentifierTypePhone,
			Value: "+9647500000040",
		},
		Purpose:   auth.OTPPurposeLogin,
		CodeHash:  "integration-test-code-hash",
		ExpiresAt: now.Add(10 * time.Minute),
	}

	_, err = pool.Exec(
		ctx,
		`
			DELETE FROM otp_challenges
			WHERE id = $1
		`,
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
			`
				DELETE FROM otp_challenges
				WHERE id = $1
			`,
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

	cancelledAt := now.Add(time.Second)

	if err := repository.Cancel(
		ctx,
		challenge.ID,
		cancelledAt,
	); err != nil {
		t.Fatalf(
			"Cancel() returned an error: %v",
			err,
		)
	}

	storedChallenge, err := repository.FindByID(
		ctx,
		challenge.ID,
	)
	if err != nil {
		t.Fatalf(
			"FindByID() returned an error: %v",
			err,
		)
	}

	if storedChallenge.CancelledAt == nil {
		t.Fatal(
			"cancelled challenge has nil CancelledAt",
		)
	}

	storedCancelledAt := storedChallenge.CancelledAt.
		UTC().
		Truncate(time.Microsecond)

	expectedCancelledAt := cancelledAt.
		UTC().
		Truncate(time.Microsecond)

	if !storedCancelledAt.Equal(
		expectedCancelledAt,
	) {
		t.Fatalf(
			"CancelledAt is %v, expected %v",
			storedCancelledAt,
			expectedCancelledAt,
		)
	}

	if storedChallenge.VerifiedAt != nil {
		t.Fatal(
			"cancelled challenge was unexpectedly verified",
		)
	}

	if err := repository.Cancel(
		ctx,
		challenge.ID,
		cancelledAt.Add(time.Second),
	); err != nil {
		t.Fatalf(
			"second Cancel() returned an error: %v",
			err,
		)
	}

	err = repository.RecordFailedAttempt(
		ctx,
		challenge.ID,
		cancelledAt.Add(2*time.Second),
	)

	if !errors.Is(
		err,
		auth.ErrChallengeCancelled,
	) {
		t.Fatalf(
			"RecordFailedAttempt() returned %v, expected %v",
			err,
			auth.ErrChallengeCancelled,
		)
	}

	storedChallenge, err = repository.FindByID(
		ctx,
		challenge.ID,
	)
	if err != nil {
		t.Fatalf(
			"FindByID() after failed attempt returned an error: %v",
			err,
		)
	}

	if storedChallenge.FailedAttempts != 0 {
		t.Fatalf(
			"failed attempts is %d, expected 0",
			storedChallenge.FailedAttempts,
		)
	}

	err = repository.MarkVerified(
		ctx,
		challenge.ID,
		cancelledAt.Add(3*time.Second),
	)

	if !errors.Is(
		err,
		auth.ErrChallengeCancelled,
	) {
		t.Fatalf(
			"MarkVerified() returned %v, expected %v",
			err,
			auth.ErrChallengeCancelled,
		)
	}
}

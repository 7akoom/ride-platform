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
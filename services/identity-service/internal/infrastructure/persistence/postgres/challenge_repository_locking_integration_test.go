//go:build integration

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

func TestChallengeRepositoryUsesCancellationTimeAfterWaitingForScopeLock(
	t *testing.T,
) {
	ctx, pool, repository := newChallengeRepositoryIntegrationTest(t)

	const existingChallengeID = "otp_ch_lock_wait_existing"
	const newChallengeID = "otp_ch_lock_wait_new"

	identifier := auth.Identifier{
		Type:  auth.IdentifierTypePhone,
		Value: "+9647500000098",
	}

	purpose := auth.OTPPurposeLogin

	cleanupChallengeIDs(
		t,
		pool,
		existingChallengeID,
		newChallengeID,
	)

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

	lockKey := challengeScopeLockKey(
		identifier,
		purpose,
		nil,
		nil,
	)

	_, err = lockTx.Exec(
		ctx,
		`
			SELECT pg_advisory_xact_lock(
				hashtextextended($1, 0)
			)
		`,
		lockKey,
	)
	if err != nil {
		t.Fatalf(
			"acquire advisory challenge scope lock: %v",
			err,
		)
	}

	createResult := make(chan error, 1)

	newChallenge := auth.OTPChallenge{
		ID:         newChallengeID,
		Identifier: identifier,
		Purpose:    purpose,
		CodeHash:   "new-lock-wait-code-hash",
		ExpiresAt: time.Now().
			UTC().
			Add(5 * time.Minute),
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
				identifier_type,
				normalized_value,
				purpose,
				target_identity_id,
				code_hash,
				expires_at
			)
			VALUES (
				$1,
				$2,
				$3,
				$4,
				NULL,
				$5,
				$6
			)
		`,
		existingChallengeID,
		string(identifier.Type),
		identifier.Value,
		string(purpose),
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
			"release advisory challenge scope lock: %v",
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

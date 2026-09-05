//go:build integration

package postgres

import (
	"sync"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

func TestChallengeRepositoryKeepsOnlyLatestChallengeActiveConcurrently(
	t *testing.T,
) {
	ctx, pool, repository := newChallengeRepositoryIntegrationTest(t)

	const firstChallengeID = "otp_ch_concurrent_latest_first"
	const secondChallengeID = "otp_ch_concurrent_latest_second"

	identifier := auth.Identifier{
		Type:  auth.IdentifierTypeEmail,
		Value: "latest@example.com",
	}

	cleanupChallengeIDs(
		t,
		pool,
		firstChallengeID,
		secondChallengeID,
	)

	now := time.Now().UTC()

	firstChallenge := auth.OTPChallenge{
		ID:         firstChallengeID,
		Identifier: identifier,
		Purpose:    auth.OTPPurposeLogin,
		CodeHash:   "first-concurrent-code-hash",
		ExpiresAt:  now.Add(5 * time.Minute),
	}

	secondChallenge := auth.OTPChallenge{
		ID:         secondChallengeID,
		Identifier: identifier,
		Purpose:    auth.OTPPurposeLogin,
		CodeHash:   "second-concurrent-code-hash",
		ExpiresAt:  now.Add(5 * time.Minute),
	}

	start := make(chan struct{})
	results := make(chan error, 2)

	var waitGroup sync.WaitGroup
	waitGroup.Add(2)

	go func() {
		defer waitGroup.Done()

		<-start

		results <- repository.Create(
			ctx,
			firstChallenge,
		)
	}()

	go func() {
		defer waitGroup.Done()

		<-start

		results <- repository.Create(
			ctx,
			secondChallenge,
		)
	}()

	close(start)

	waitGroup.Wait()
	close(results)

	for createErr := range results {
		if createErr != nil {
			t.Fatalf(
				"concurrent Create() returned an error: %v",
				createErr,
			)
		}
	}

	var totalChallenges int
	var activeChallenges int
	var cancelledChallenges int

	err := pool.QueryRow(
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
			"total challenges = %d, want 2",
			totalChallenges,
		)
	}

	if activeChallenges != 1 {
		t.Fatalf(
			"active challenges = %d, want 1",
			activeChallenges,
		)
	}

	if cancelledChallenges != 1 {
		t.Fatalf(
			"cancelled challenges = %d, want 1",
			cancelledChallenges,
		)
	}
}

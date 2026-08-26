//go:build integration

package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

func TestIdentifierUnlinkCompletionStoreSerializesCompetingLastIdentifiers(
	t *testing.T,
) {
	ctx, pool, requestStore :=
		newIdentifierUnlinkRequestIntegrationTest(t)

	completionStore :=
		NewIdentifierUnlinkCompletionStore(pool)

	identityID := createIdentifierUnlinkTestIdentity(
		t,
		ctx,
		pool,
	)

	firstIdentifier := auth.Identifier{
		Type:  auth.IdentifierTypePhone,
		Value: "+9647500000508",
	}

	secondIdentifier := auth.Identifier{
		Type:  auth.IdentifierTypeEmail,
		Value: "unlink-completion-race@example.com",
	}

	insertIdentifierUnlinkTestIdentifier(
		t,
		ctx,
		pool,
		identityID,
		firstIdentifier,
	)

	insertIdentifierUnlinkTestIdentifier(
		t,
		ctx,
		pool,
		identityID,
		secondIdentifier,
	)

	const firstChallengeID = "otp_ch_unlink_completion_race_first"

	const secondChallengeID = "otp_ch_unlink_completion_race_second"

	createIdentifierUnlinkCompletionRequest(
		t,
		ctx,
		requestStore,
		identityID,
		firstChallengeID,
		firstIdentifier,
		secondIdentifier,
	)

	createIdentifierUnlinkCompletionRequest(
		t,
		ctx,
		requestStore,
		identityID,
		secondChallengeID,
		secondIdentifier,
		firstIdentifier,
	)

	start := make(chan struct{})
	results := make(chan error, 2)

	var waitGroup sync.WaitGroup
	waitGroup.Add(2)

	runComplete := func(
		challengeID string,
	) {
		defer waitGroup.Done()

		<-start

		results <- completionStore.Complete(
			context.Background(),
			auth.IdentifierUnlinkCompletionInput{
				ChallengeID: challengeID,
				IdentityID:  identityID,
				VerifiedAt:  time.Now().UTC(),
			},
		)
	}

	go runComplete(firstChallengeID)
	go runComplete(secondChallengeID)

	close(start)

	waitGroup.Wait()
	close(results)

	successCount := 0
	safeFailureCount := 0

	for completeErr := range results {
		switch {
		case completeErr == nil:
			successCount++

		case errors.Is(
			completeErr,
			auth.ErrIdentifierNotLinked,
		):
			safeFailureCount++

		case errors.Is(
			completeErr,
			auth.ErrLastIdentifierRemoval,
		):
			safeFailureCount++

		default:
			t.Fatalf(
				"unexpected concurrent Complete() error: %v",
				completeErr,
			)
		}
	}

	if successCount != 1 {
		t.Fatalf(
			"successful completions = %d, want 1",
			successCount,
		)
	}

	if safeFailureCount != 1 {
		t.Fatalf(
			"safe failed completions = %d, want 1",
			safeFailureCount,
		)
	}

	var remainingIdentifierCount int

	err := pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM identity_identifiers
			WHERE identity_id = $1::uuid
		`,
		identityID,
	).Scan(
		&remainingIdentifierCount,
	)
	if err != nil {
		t.Fatalf(
			"count remaining identity identifiers: %v",
			err,
		)
	}

	if remainingIdentifierCount != 1 {
		t.Fatalf(
			"remaining identifiers = %d, want 1",
			remainingIdentifierCount,
		)
	}

	var verifiedChallengeCount int

	err = pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM otp_challenges
			WHERE id IN ($1, $2)
			  AND verified_at IS NOT NULL
		`,
		firstChallengeID,
		secondChallengeID,
	).Scan(
		&verifiedChallengeCount,
	)
	if err != nil {
		t.Fatalf(
			"count verified competing unlink challenges: %v",
			err,
		)
	}

	if verifiedChallengeCount != 1 {
		t.Fatalf(
			"verified competing challenges = %d, want 1",
			verifiedChallengeCount,
		)
	}
}

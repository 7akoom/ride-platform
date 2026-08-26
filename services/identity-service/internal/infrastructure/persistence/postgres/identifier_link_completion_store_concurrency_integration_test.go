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

func TestIdentifierLinkCompletionStoreSerializesCompetingOwners(
	t *testing.T,
) {
	ctx, pool, store, challengeRepository :=
		newIdentifierLinkCompletionIntegrationTest(t)

	firstIdentityID := createIdentifierLinkTestIdentity(
		t,
		ctx,
		pool,
	)

	secondIdentityID := createIdentifierLinkTestIdentity(
		t,
		ctx,
		pool,
	)

	identifier := auth.Identifier{
		Type:  auth.IdentifierTypeEmail,
		Value: "race@example.com",
	}

	const firstChallengeID = "otp_ch_link_completion_race_first"
	const secondChallengeID = "otp_ch_link_completion_race_second"

	firstChallenge := auth.OTPChallenge{
		ID:               firstChallengeID,
		Identifier:       identifier,
		Purpose:          auth.OTPPurposeLinkIdentifier,
		TargetIdentityID: &firstIdentityID,
		CodeHash:         "first-race-hash",
		ExpiresAt: time.Now().
			UTC().
			Add(5 * time.Minute),
	}

	secondChallenge := auth.OTPChallenge{
		ID:               secondChallengeID,
		Identifier:       identifier,
		Purpose:          auth.OTPPurposeLinkIdentifier,
		TargetIdentityID: &secondIdentityID,
		CodeHash:         "second-race-hash",
		ExpiresAt: time.Now().
			UTC().
			Add(5 * time.Minute),
	}

	for _, challenge := range []auth.OTPChallenge{
		firstChallenge,
		secondChallenge,
	} {
		if err := challengeRepository.Create(
			ctx,
			challenge,
		); err != nil {
			t.Fatalf(
				"create competing challenge %q: %v",
				challenge.ID,
				err,
			)
		}
	}

	start := make(chan struct{})
	results := make(chan error, 2)

	var waitGroup sync.WaitGroup
	waitGroup.Add(2)

	runComplete := func(
		challengeID string,
		identityID string,
	) {
		defer waitGroup.Done()

		<-start

		results <- store.Complete(
			context.Background(),
			auth.IdentifierLinkCompletionInput{
				ChallengeID: challengeID,
				IdentityID:  identityID,
				Identifier:  identifier,
				VerifiedAt:  time.Now().UTC(),
			},
		)
	}

	go runComplete(
		firstChallengeID,
		firstIdentityID,
	)

	go runComplete(
		secondChallengeID,
		secondIdentityID,
	)

	close(start)

	waitGroup.Wait()
	close(results)

	var successCount int
	var conflictCount int

	for completeErr := range results {
		switch {
		case completeErr == nil:
			successCount++

		case errors.Is(
			completeErr,
			auth.ErrIdentifierAlreadyLinked,
		):
			conflictCount++

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

	if conflictCount != 1 {
		t.Fatalf(
			"ownership conflicts = %d, want 1",
			conflictCount,
		)
	}

	var identifierCount int

	err := pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM identity_identifiers
			WHERE identifier_type = $1
			  AND normalized_value = $2
		`,
		string(identifier.Type),
		identifier.Value,
	).Scan(
		&identifierCount,
	)
	if err != nil {
		t.Fatalf(
			"count competing identifier rows: %v",
			err,
		)
	}

	if identifierCount != 1 {
		t.Fatalf(
			"identifier row count after race = %d, want 1",
			identifierCount,
		)
	}

	var verifiedChallenges int

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
		&verifiedChallenges,
	)
	if err != nil {
		t.Fatalf(
			"count verified competing challenges: %v",
			err,
		)
	}

	if verifiedChallenges != 1 {
		t.Fatalf(
			"verified competing challenges = %d, want 1",
			verifiedChallenges,
		)
	}
}

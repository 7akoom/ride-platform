//go:build integration

package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIdentifierUnlinkCompletionStoreCompletesAtomically(
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

	targetIdentifier := auth.Identifier{
		Type:  auth.IdentifierTypePhone,
		Value: "+9647500000501",
	}

	verificationIdentifier := auth.Identifier{
		Type:  auth.IdentifierTypeEmail,
		Value: "unlink-completion-success@example.com",
	}

	insertIdentifierUnlinkTestIdentifier(
		t,
		ctx,
		pool,
		identityID,
		targetIdentifier,
	)

	insertIdentifierUnlinkTestIdentifier(
		t,
		ctx,
		pool,
		identityID,
		verificationIdentifier,
	)

	const challengeID = "otp_ch_unlink_completion_success"

	createIdentifierUnlinkCompletionRequest(
		t,
		ctx,
		requestStore,
		identityID,
		challengeID,
		targetIdentifier,
		verificationIdentifier,
	)

	err := completionStore.Complete(
		ctx,
		auth.IdentifierUnlinkCompletionInput{
			ChallengeID: challengeID,
			IdentityID:  identityID,
			VerifiedAt:  time.Now().UTC(),
		},
	)
	if err != nil {
		t.Fatalf(
			"Complete() returned an error: %v",
			err,
		)
	}

	if countIdentifierUnlinkCompletionIdentifier(
		t,
		ctx,
		pool,
		identityID,
		targetIdentifier,
	) != 0 {
		t.Fatal(
			"target identifier still exists after unlink completion",
		)
	}

	if countIdentifierUnlinkCompletionIdentifier(
		t,
		ctx,
		pool,
		identityID,
		verificationIdentifier,
	) != 1 {
		t.Fatal(
			"verification identifier was removed during unlink completion",
		)
	}

	var challengeVerifiedAt *time.Time

	err = pool.QueryRow(
		ctx,
		`
			SELECT verified_at
			FROM otp_challenges
			WHERE id = $1
		`,
		challengeID,
	).Scan(
		&challengeVerifiedAt,
	)
	if err != nil {
		t.Fatalf(
			"query completed unlink challenge: %v",
			err,
		)
	}

	if challengeVerifiedAt == nil {
		t.Fatal(
			"unlink completion did not mark OTP challenge verified",
		)
	}

	var operationCount int

	err = pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM identifier_unlink_operations
			WHERE challenge_id = $1
		`,
		challengeID,
	).Scan(
		&operationCount,
	)
	if err != nil {
		t.Fatalf(
			"query completed unlink operation: %v",
			err,
		)
	}

	if operationCount != 1 {
		t.Fatalf(
			"unlink operation rows = %d, want 1",
			operationCount,
		)
	}
}

func TestIdentifierUnlinkCompletionStoreRejectsMissingTargetIdentifier(
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

	targetIdentifier := auth.Identifier{
		Type:  auth.IdentifierTypePhone,
		Value: "+9647500000502",
	}

	verificationIdentifier := auth.Identifier{
		Type:  auth.IdentifierTypeEmail,
		Value: "unlink-completion-target-missing@example.com",
	}

	insertIdentifierUnlinkTestIdentifier(
		t,
		ctx,
		pool,
		identityID,
		targetIdentifier,
	)

	insertIdentifierUnlinkTestIdentifier(
		t,
		ctx,
		pool,
		identityID,
		verificationIdentifier,
	)

	const challengeID = "otp_ch_unlink_completion_target_missing"

	createIdentifierUnlinkCompletionRequest(
		t,
		ctx,
		requestStore,
		identityID,
		challengeID,
		targetIdentifier,
		verificationIdentifier,
	)

	_, err := pool.Exec(
		ctx,
		`
			DELETE FROM identity_identifiers
			WHERE identity_id = $1::uuid
			  AND identifier_type = $2
			  AND normalized_value = $3
		`,
		identityID,
		string(targetIdentifier.Type),
		targetIdentifier.Value,
	)
	if err != nil {
		t.Fatalf(
			"delete target identifier before completion: %v",
			err,
		)
	}

	err = completionStore.Complete(
		ctx,
		auth.IdentifierUnlinkCompletionInput{
			ChallengeID: challengeID,
			IdentityID:  identityID,
			VerifiedAt:  time.Now().UTC(),
		},
	)

	if !errors.Is(
		err,
		auth.ErrIdentifierNotLinked,
	) {
		t.Fatalf(
			"Complete() error = %v, want %v",
			err,
			auth.ErrIdentifierNotLinked,
		)
	}

	assertIdentifierUnlinkCompletionChallengeUnverified(
		t,
		ctx,
		pool,
		challengeID,
	)

	if countIdentifierUnlinkCompletionIdentifier(
		t,
		ctx,
		pool,
		identityID,
		verificationIdentifier,
	) != 1 {
		t.Fatal(
			"verification identifier changed after failed completion",
		)
	}
}

func TestIdentifierUnlinkCompletionStoreRejectsMissingVerificationIdentifier(
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

	targetIdentifier := auth.Identifier{
		Type:  auth.IdentifierTypePhone,
		Value: "+9647500000503",
	}

	verificationIdentifier := auth.Identifier{
		Type:  auth.IdentifierTypeEmail,
		Value: "unlink-completion-verification-missing@example.com",
	}

	insertIdentifierUnlinkTestIdentifier(
		t,
		ctx,
		pool,
		identityID,
		targetIdentifier,
	)

	insertIdentifierUnlinkTestIdentifier(
		t,
		ctx,
		pool,
		identityID,
		verificationIdentifier,
	)

	const challengeID = "otp_ch_unlink_completion_verification_missing"

	createIdentifierUnlinkCompletionRequest(
		t,
		ctx,
		requestStore,
		identityID,
		challengeID,
		targetIdentifier,
		verificationIdentifier,
	)

	_, err := pool.Exec(
		ctx,
		`
			DELETE FROM identity_identifiers
			WHERE identity_id = $1::uuid
			  AND identifier_type = $2
			  AND normalized_value = $3
		`,
		identityID,
		string(verificationIdentifier.Type),
		verificationIdentifier.Value,
	)
	if err != nil {
		t.Fatalf(
			"delete verification identifier before completion: %v",
			err,
		)
	}

	err = completionStore.Complete(
		ctx,
		auth.IdentifierUnlinkCompletionInput{
			ChallengeID: challengeID,
			IdentityID:  identityID,
			VerifiedAt:  time.Now().UTC(),
		},
	)

	if !errors.Is(
		err,
		auth.ErrIdentifierNotLinked,
	) {
		t.Fatalf(
			"Complete() error = %v, want %v",
			err,
			auth.ErrIdentifierNotLinked,
		)
	}

	if countIdentifierUnlinkCompletionIdentifier(
		t,
		ctx,
		pool,
		identityID,
		targetIdentifier,
	) != 1 {
		t.Fatal(
			"target identifier was deleted after verification identifier disappeared",
		)
	}

	assertIdentifierUnlinkCompletionChallengeUnverified(
		t,
		ctx,
		pool,
		challengeID,
	)
}

func TestIdentifierUnlinkCompletionStoreRejectsInvalidChallengeStates(
	t *testing.T,
) {
	tests := []struct {
		name      string
		challenge string
		mutate    func(
			context.Context,
			*pgxpool.Pool,
			string,
		) error
		expected error
	}{
		{
			name:      "used",
			challenge: "otp_ch_unlink_completion_used",
			mutate: func(
				ctx context.Context,
				pool *pgxpool.Pool,
				challengeID string,
			) error {
				_, err := pool.Exec(
					ctx,
					`
						UPDATE otp_challenges
						SET verified_at = $1
						WHERE id = $2
					`,
					time.Now().UTC(),
					challengeID,
				)

				return err
			},
			expected: auth.ErrChallengeUsed,
		},
		{
			name:      "cancelled",
			challenge: "otp_ch_unlink_completion_cancelled",
			mutate: func(
				ctx context.Context,
				pool *pgxpool.Pool,
				challengeID string,
			) error {
				_, err := pool.Exec(
					ctx,
					`
						UPDATE otp_challenges
						SET cancelled_at = $1
						WHERE id = $2
					`,
					time.Now().UTC(),
					challengeID,
				)

				return err
			},
			expected: auth.ErrChallengeCancelled,
		},
		{
			name:      "expired",
			challenge: "otp_ch_unlink_completion_expired",
			mutate: func(
				ctx context.Context,
				pool *pgxpool.Pool,
				challengeID string,
			) error {
				return nil
			},
			expected: auth.ErrChallengeExpired,
		},
	}

	for index, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, pool, requestStore :=
				newIdentifierUnlinkRequestIntegrationTest(t)

			completionStore :=
				NewIdentifierUnlinkCompletionStore(pool)

			identityID :=
				createIdentifierUnlinkTestIdentity(
					t,
					ctx,
					pool,
				)

			targetIdentifier := auth.Identifier{
				Type: auth.IdentifierTypePhone,
				Value: []string{
					"+9647500000504",
					"+9647500000505",
					"+9647500000506",
				}[index],
			}

			verificationIdentifier := auth.Identifier{
				Type: auth.IdentifierTypeEmail,
				Value: []string{
					"unlink-completion-used@example.com",
					"unlink-completion-cancelled@example.com",
					"unlink-completion-expired@example.com",
				}[index],
			}

			insertIdentifierUnlinkTestIdentifier(
				t,
				ctx,
				pool,
				identityID,
				targetIdentifier,
			)

			insertIdentifierUnlinkTestIdentifier(
				t,
				ctx,
				pool,
				identityID,
				verificationIdentifier,
			)

			createIdentifierUnlinkCompletionRequest(
				t,
				ctx,
				requestStore,
				identityID,
				tt.challenge,
				targetIdentifier,
				verificationIdentifier,
			)

			if err := tt.mutate(
				ctx,
				pool,
				tt.challenge,
			); err != nil {
				t.Fatalf(
					"mutate challenge state: %v",
					err,
				)
			}

			verifiedAt := time.Now().UTC()

			if tt.name == "expired" {
				verifiedAt = verifiedAt.Add(10 * time.Minute)
			}

			err := completionStore.Complete(
				ctx,
				auth.IdentifierUnlinkCompletionInput{
					ChallengeID: tt.challenge,
					IdentityID:  identityID,
					VerifiedAt:  verifiedAt,
				},
			)

			if !errors.Is(err, tt.expected) {
				t.Fatalf(
					"Complete() error = %v, want %v",
					err,
					tt.expected,
				)
			}

			if countIdentifierUnlinkCompletionIdentifier(
				t,
				ctx,
				pool,
				identityID,
				targetIdentifier,
			) != 1 {
				t.Fatal(
					"failed completion deleted target identifier",
				)
			}
		})
	}
}

func TestIdentifierUnlinkCompletionStoreRejectsWrongPurposeAndRollsBack(
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

	targetIdentifier := auth.Identifier{
		Type:  auth.IdentifierTypePhone,
		Value: "+9647500000507",
	}

	verificationIdentifier := auth.Identifier{
		Type:  auth.IdentifierTypeEmail,
		Value: "unlink-completion-purpose@example.com",
	}

	insertIdentifierUnlinkTestIdentifier(
		t,
		ctx,
		pool,
		identityID,
		targetIdentifier,
	)

	insertIdentifierUnlinkTestIdentifier(
		t,
		ctx,
		pool,
		identityID,
		verificationIdentifier,
	)

	const challengeID = "otp_ch_unlink_completion_wrong_purpose"

	createIdentifierUnlinkCompletionRequest(
		t,
		ctx,
		requestStore,
		identityID,
		challengeID,
		targetIdentifier,
		verificationIdentifier,
	)

	_, err := pool.Exec(
		ctx,
		`
			UPDATE otp_challenges
			SET purpose = $1
			WHERE id = $2
		`,
		string(auth.OTPPurposeLinkIdentifier),
		challengeID,
	)
	if err != nil {
		t.Fatalf(
			"change challenge purpose: %v",
			err,
		)
	}

	err = completionStore.Complete(
		ctx,
		auth.IdentifierUnlinkCompletionInput{
			ChallengeID: challengeID,
			IdentityID:  identityID,
			VerifiedAt:  time.Now().UTC(),
		},
	)
	if err == nil {
		t.Fatal(
			"Complete() accepted a non-unlink challenge",
		)
	}

	if countIdentifierUnlinkCompletionIdentifier(
		t,
		ctx,
		pool,
		identityID,
		targetIdentifier,
	) != 1 {
		t.Fatal(
			"wrong-purpose completion deleted target identifier",
		)
	}

	assertIdentifierUnlinkCompletionChallengeUnverified(
		t,
		ctx,
		pool,
		challengeID,
	)
}

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

func createIdentifierUnlinkCompletionRequest(
	t *testing.T,
	ctx context.Context,
	requestStore *IdentifierUnlinkRequestStore,
	identityID string,
	challengeID string,
	targetIdentifier auth.Identifier,
	verificationIdentifier auth.Identifier,
) {
	t.Helper()

	err := requestStore.Create(
		ctx,
		auth.IdentifierUnlinkRequestInput{
			Challenge: auth.OTPChallenge{
				ID:               challengeID,
				Identifier:       verificationIdentifier,
				Purpose:          auth.OTPPurposeUnlinkIdentifier,
				TargetIdentityID: &identityID,
				CodeHash:         challengeID + "-hash",
				ExpiresAt: time.Now().
					UTC().
					Add(5 * time.Minute),
			},
			TargetIdentifier: targetIdentifier,
		},
	)
	if err != nil {
		t.Fatalf(
			"create identifier unlink request %q: %v",
			challengeID,
			err,
		)
	}
}

func countIdentifierUnlinkCompletionIdentifier(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	identityID string,
	identifier auth.Identifier,
) int {
	t.Helper()

	var count int

	err := pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM identity_identifiers
			WHERE identity_id = $1::uuid
			  AND identifier_type = $2
			  AND normalized_value = $3
		`,
		identityID,
		string(identifier.Type),
		identifier.Value,
	).Scan(
		&count,
	)
	if err != nil {
		t.Fatalf(
			"count identity identifier: %v",
			err,
		)
	}

	return count
}

func assertIdentifierUnlinkCompletionChallengeUnverified(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	challengeID string,
) {
	t.Helper()

	var verifiedAt *time.Time

	err := pool.QueryRow(
		ctx,
		`
			SELECT verified_at
			FROM otp_challenges
			WHERE id = $1
		`,
		challengeID,
	).Scan(
		&verifiedAt,
	)
	if err != nil {
		t.Fatalf(
			"query unlink challenge verification state: %v",
			err,
		)
	}

	if verifiedAt != nil {
		t.Fatal(
			"failed unlink completion consumed the OTP challenge",
		)
	}
}

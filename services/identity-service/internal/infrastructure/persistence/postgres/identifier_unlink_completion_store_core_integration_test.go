//go:build integration

package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
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

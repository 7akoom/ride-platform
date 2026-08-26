//go:build integration

package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

func TestIdentifierLinkCompletionStoreOwnershipConflictDoesNotConsumeChallenge(
	t *testing.T,
) {
	ctx, pool, store, challengeRepository :=
		newIdentifierLinkCompletionIntegrationTest(t)

	ownerIdentityID := createIdentifierLinkTestIdentity(
		t,
		ctx,
		pool,
	)

	targetIdentityID := createIdentifierLinkTestIdentity(
		t,
		ctx,
		pool,
	)

	identifier := auth.Identifier{
		Type:  auth.IdentifierTypeEmail,
		Value: "owned@example.com",
	}

	_, err := pool.Exec(
		ctx,
		`
			INSERT INTO identity_identifiers (
				identity_id,
				identifier_type,
				normalized_value,
				verified_at
			)
			VALUES ($1, $2, $3, $4)
		`,
		ownerIdentityID,
		string(identifier.Type),
		identifier.Value,
		time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf(
			"seed existing identifier owner: %v",
			err,
		)
	}

	const challengeID = "otp_ch_link_completion_conflict"

	challenge := auth.OTPChallenge{
		ID:               challengeID,
		Identifier:       identifier,
		Purpose:          auth.OTPPurposeLinkIdentifier,
		TargetIdentityID: &targetIdentityID,
		CodeHash:         "link-completion-conflict-hash",
		ExpiresAt: time.Now().
			UTC().
			Add(5 * time.Minute),
	}

	if err := challengeRepository.Create(
		ctx,
		challenge,
	); err != nil {
		t.Fatalf(
			"create conflicting link challenge: %v",
			err,
		)
	}

	err = store.Complete(
		ctx,
		auth.IdentifierLinkCompletionInput{
			ChallengeID: challengeID,
			IdentityID:  targetIdentityID,
			Identifier:  identifier,
			VerifiedAt:  time.Now().UTC(),
		},
	)

	if !errors.Is(
		err,
		auth.ErrIdentifierAlreadyLinked,
	) {
		t.Fatalf(
			"Complete() error = %v, want %v",
			err,
			auth.ErrIdentifierAlreadyLinked,
		)
	}

	var storedOwnerIdentityID string

	err = pool.QueryRow(
		ctx,
		`
			SELECT identity_id::text
			FROM identity_identifiers
			WHERE identifier_type = $1
			  AND normalized_value = $2
		`,
		string(identifier.Type),
		identifier.Value,
	).Scan(
		&storedOwnerIdentityID,
	)
	if err != nil {
		t.Fatalf(
			"query existing identifier owner: %v",
			err,
		)
	}

	if storedOwnerIdentityID != ownerIdentityID {
		t.Fatalf(
			"identifier owner changed to %q, want %q",
			storedOwnerIdentityID,
			ownerIdentityID,
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
			"query conflicting challenge state: %v",
			err,
		)
	}

	if challengeVerifiedAt != nil {
		t.Fatal(
			"ownership conflict consumed the OTP challenge",
		)
	}
}

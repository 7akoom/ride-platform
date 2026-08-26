//go:build integration

package postgres

import (
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

func TestIdentifierLinkCompletionStoreLinksIdentifierAndConsumesChallenge(
	t *testing.T,
) {
	ctx, pool, store, challengeRepository :=
		newIdentifierLinkCompletionIntegrationTest(t)

	identityID := createIdentifierLinkTestIdentity(
		t,
		ctx,
		pool,
	)

	const challengeID = "otp_ch_link_completion_success"

	identifier := auth.Identifier{
		Type:  auth.IdentifierTypeEmail,
		Value: "Link.Success@EXAMPLE.COM",
	}

	challenge := auth.OTPChallenge{
		ID:               challengeID,
		Identifier:       identifier,
		Purpose:          auth.OTPPurposeLinkIdentifier,
		TargetIdentityID: &identityID,
		CodeHash:         "link-completion-success-hash",
		ExpiresAt: time.Now().
			UTC().
			Add(5 * time.Minute),
	}

	if err := challengeRepository.Create(
		ctx,
		challenge,
	); err != nil {
		t.Fatalf(
			"create link identifier challenge: %v",
			err,
		)
	}

	verifiedAt := time.Now().UTC()

	err := store.Complete(
		ctx,
		auth.IdentifierLinkCompletionInput{
			ChallengeID: challengeID,
			IdentityID:  identityID,
			Identifier:  identifier,
			VerifiedAt:  verifiedAt,
		},
	)
	if err != nil {
		t.Fatalf(
			"Complete() returned an error: %v",
			err,
		)
	}

	var storedIdentityID string
	var normalizedValue string
	var identifierVerifiedAt time.Time

	err = pool.QueryRow(
		ctx,
		`
			SELECT
				identity_id::text,
				normalized_value,
				verified_at
			FROM identity_identifiers
			WHERE identifier_type = $1
			  AND normalized_value = $2
		`,
		string(auth.IdentifierTypeEmail),
		"link.success@example.com",
	).Scan(
		&storedIdentityID,
		&normalizedValue,
		&identifierVerifiedAt,
	)
	if err != nil {
		t.Fatalf(
			"query linked identifier: %v",
			err,
		)
	}

	if storedIdentityID != identityID {
		t.Fatalf(
			"linked identity ID = %q, want %q",
			storedIdentityID,
			identityID,
		)
	}

	if normalizedValue != "link.success@example.com" {
		t.Fatalf(
			"normalized identifier = %q, want %q",
			normalizedValue,
			"link.success@example.com",
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
			"query completed challenge: %v",
			err,
		)
	}

	if challengeVerifiedAt == nil {
		t.Fatal(
			"identifier was linked but challenge was not consumed",
		)
	}

	if identifierVerifiedAt.IsZero() {
		t.Fatal(
			"linked identifier has zero verification time",
		)
	}
}
func TestIdentifierLinkCompletionStoreAllowsExistingLinkForSameIdentity(
	t *testing.T,
) {
	ctx, pool, store, challengeRepository :=
		newIdentifierLinkCompletionIntegrationTest(t)

	identityID := createIdentifierLinkTestIdentity(
		t,
		ctx,
		pool,
	)

	identifier := auth.Identifier{
		Type:  auth.IdentifierTypePhone,
		Value: "+9647500000088",
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
		identityID,
		string(identifier.Type),
		identifier.Value,
		time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf(
			"seed identifier already linked to same identity: %v",
			err,
		)
	}

	const challengeID = "otp_ch_link_completion_idempotent"

	challenge := auth.OTPChallenge{
		ID:               challengeID,
		Identifier:       identifier,
		Purpose:          auth.OTPPurposeLinkIdentifier,
		TargetIdentityID: &identityID,
		CodeHash:         "link-completion-idempotent-hash",
		ExpiresAt: time.Now().
			UTC().
			Add(5 * time.Minute),
	}

	if err := challengeRepository.Create(
		ctx,
		challenge,
	); err != nil {
		t.Fatalf(
			"create idempotent link challenge: %v",
			err,
		)
	}

	err = store.Complete(
		ctx,
		auth.IdentifierLinkCompletionInput{
			ChallengeID: challengeID,
			IdentityID:  identityID,
			Identifier:  identifier,
			VerifiedAt:  time.Now().UTC(),
		},
	)
	if err != nil {
		t.Fatalf(
			"Complete() for existing same-identity link returned: %v",
			err,
		)
	}

	var identifierCount int

	err = pool.QueryRow(
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
			"count identifier rows: %v",
			err,
		)
	}

	if identifierCount != 1 {
		t.Fatalf(
			"identifier row count = %d, want 1",
			identifierCount,
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
			"query idempotent challenge state: %v",
			err,
		)
	}

	if challengeVerifiedAt == nil {
		t.Fatal(
			"idempotent link did not consume its OTP challenge",
		)
	}
}

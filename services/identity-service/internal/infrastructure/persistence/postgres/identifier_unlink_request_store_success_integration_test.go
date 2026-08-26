//go:build integration

package postgres

import (
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

func TestIdentifierUnlinkRequestStoreCreatesChallengeAndOperationAtomically(
	t *testing.T,
) {
	ctx, pool, store := newIdentifierUnlinkRequestIntegrationTest(t)

	identityID := createIdentifierUnlinkTestIdentity(
		t,
		ctx,
		pool,
	)

	targetIdentifier := auth.Identifier{
		Type:  auth.IdentifierTypePhone,
		Value: "+9647500000301",
	}

	verificationIdentifier := auth.Identifier{
		Type:  auth.IdentifierTypeEmail,
		Value: "unlink-request-success@example.com",
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

	const challengeID = "otp_ch_unlink_request_success"

	challenge := auth.OTPChallenge{
		ID:               challengeID,
		Identifier:       verificationIdentifier,
		Purpose:          auth.OTPPurposeUnlinkIdentifier,
		TargetIdentityID: &identityID,
		CodeHash:         "unlink-request-success-hash",
		ExpiresAt: time.Now().
			UTC().
			Add(5 * time.Minute),
	}

	err := store.Create(
		ctx,
		auth.IdentifierUnlinkRequestInput{
			Challenge:        challenge,
			TargetIdentifier: targetIdentifier,
		},
	)
	if err != nil {
		t.Fatalf(
			"Create() returned an error: %v",
			err,
		)
	}

	var (
		storedVerificationType  string
		storedVerificationValue string
		storedPurpose           string
		storedTargetIdentityID  string
	)

	err = pool.QueryRow(
		ctx,
		`
			SELECT
				identifier_type,
				normalized_value,
				purpose,
				target_identity_id::text
			FROM otp_challenges
			WHERE id = $1
		`,
		challengeID,
	).Scan(
		&storedVerificationType,
		&storedVerificationValue,
		&storedPurpose,
		&storedTargetIdentityID,
	)
	if err != nil {
		t.Fatalf(
			"query created unlink OTP challenge: %v",
			err,
		)
	}

	if storedVerificationType != string(auth.IdentifierTypeEmail) {
		t.Fatalf(
			"verification identifier type = %q, want %q",
			storedVerificationType,
			auth.IdentifierTypeEmail,
		)
	}

	if storedVerificationValue != verificationIdentifier.Value {
		t.Fatalf(
			"verification identifier value = %q, want %q",
			storedVerificationValue,
			verificationIdentifier.Value,
		)
	}

	if storedPurpose != string(auth.OTPPurposeUnlinkIdentifier) {
		t.Fatalf(
			"OTP purpose = %q, want %q",
			storedPurpose,
			auth.OTPPurposeUnlinkIdentifier,
		)
	}

	if storedTargetIdentityID != identityID {
		t.Fatalf(
			"OTP target identity ID = %q, want %q",
			storedTargetIdentityID,
			identityID,
		)
	}

	var (
		operationIdentityID      string
		operationIdentifierType  string
		operationNormalizedValue string
	)

	err = pool.QueryRow(
		ctx,
		`
			SELECT
				identity_id::text,
				identifier_type,
				normalized_value
			FROM identifier_unlink_operations
			WHERE challenge_id = $1
		`,
		challengeID,
	).Scan(
		&operationIdentityID,
		&operationIdentifierType,
		&operationNormalizedValue,
	)
	if err != nil {
		t.Fatalf(
			"query created identifier unlink operation: %v",
			err,
		)
	}

	if operationIdentityID != identityID {
		t.Fatalf(
			"unlink operation identity ID = %q, want %q",
			operationIdentityID,
			identityID,
		)
	}

	if operationIdentifierType != string(targetIdentifier.Type) {
		t.Fatalf(
			"unlink operation identifier type = %q, want %q",
			operationIdentifierType,
			targetIdentifier.Type,
		)
	}

	if operationNormalizedValue != targetIdentifier.Value {
		t.Fatalf(
			"unlink operation identifier value = %q, want %q",
			operationNormalizedValue,
			targetIdentifier.Value,
		)
	}
}

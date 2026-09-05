//go:build integration

package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

func TestChallengeRepositoryCreateFindAndConsumePhoneLoginChallenge(
	t *testing.T,
) {
	ctx, pool, repository := newChallengeRepositoryIntegrationTest(t)

	const challengeID = "otp_ch_integration_phone_single_use"

	identifier := auth.Identifier{
		Type:  auth.IdentifierTypePhone,
		Value: "+9647500000001",
	}

	cleanupChallengeIDs(
		t,
		pool,
		challengeID,
	)

	now := time.Now().UTC()

	challenge := auth.OTPChallenge{
		ID:         challengeID,
		Identifier: identifier,
		Purpose:    auth.OTPPurposeLogin,
		CodeHash:   "integration-test-code-hash",
		ExpiresAt:  now.Add(5 * time.Minute),
	}

	if err := repository.Create(
		ctx,
		challenge,
	); err != nil {
		t.Fatalf(
			"Create() returned an error: %v",
			err,
		)
	}

	storedChallenge, err := repository.FindByID(
		ctx,
		challengeID,
	)
	if err != nil {
		t.Fatalf(
			"FindByID() returned an error: %v",
			err,
		)
	}

	if storedChallenge.ID != challenge.ID {
		t.Fatalf(
			"FindByID() returned ID %q, want %q",
			storedChallenge.ID,
			challenge.ID,
		)
	}

	if storedChallenge.Identifier != identifier {
		t.Fatalf(
			"FindByID() returned identifier %+v, want %+v",
			storedChallenge.Identifier,
			identifier,
		)
	}

	if storedChallenge.Purpose != auth.OTPPurposeLogin {
		t.Fatalf(
			"FindByID() returned purpose %q, want %q",
			storedChallenge.Purpose,
			auth.OTPPurposeLogin,
		)
	}

	if storedChallenge.TargetIdentityID != nil {
		t.Fatalf(
			"login challenge target identity = %v, want nil",
			storedChallenge.TargetIdentityID,
		)
	}

	if storedChallenge.CodeHash != challenge.CodeHash {
		t.Fatal(
			"FindByID() returned unexpected code hash",
		)
	}

	if storedChallenge.VerifiedAt != nil {
		t.Fatal(
			"new challenge is already marked as verified",
		)
	}

	verifiedAt := time.Now().UTC()

	if err := repository.MarkVerified(
		ctx,
		challengeID,
		verifiedAt,
	); err != nil {
		t.Fatalf(
			"first MarkVerified() returned an error: %v",
			err,
		)
	}

	err = repository.MarkVerified(
		ctx,
		challengeID,
		verifiedAt.Add(time.Second),
	)

	if !errors.Is(
		err,
		auth.ErrChallengeUsed,
	) {
		t.Fatalf(
			"second MarkVerified() error = %v, want %v",
			err,
			auth.ErrChallengeUsed,
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
		t.Fatal(
			"consumed challenge has nil VerifiedAt",
		)
	}
}

func TestChallengeRepositoryNormalizesAndRestoresEmailLoginChallenge(
	t *testing.T,
) {
	ctx, pool, repository := newChallengeRepositoryIntegrationTest(t)

	const challengeID = "otp_ch_integration_email_login"

	cleanupChallengeIDs(
		t,
		pool,
		challengeID,
	)

	challenge := auth.OTPChallenge{
		ID: challengeID,
		Identifier: auth.Identifier{
			Type:  auth.IdentifierTypeEmail,
			Value: "Login.User@EXAMPLE.COM",
		},
		Purpose:  auth.OTPPurposeLogin,
		CodeHash: "email-login-code-hash",
		ExpiresAt: time.Now().
			UTC().
			Add(5 * time.Minute),
	}

	if err := repository.Create(
		ctx,
		challenge,
	); err != nil {
		t.Fatalf(
			"Create() returned an error: %v",
			err,
		)
	}

	storedChallenge, err := repository.FindByID(
		ctx,
		challengeID,
	)
	if err != nil {
		t.Fatalf(
			"FindByID() returned an error: %v",
			err,
		)
	}

	expectedIdentifier := auth.Identifier{
		Type:  auth.IdentifierTypeEmail,
		Value: "login.user@example.com",
	}

	if storedChallenge.Identifier != expectedIdentifier {
		t.Fatalf(
			"stored identifier = %+v, want %+v",
			storedChallenge.Identifier,
			expectedIdentifier,
		)
	}

	if storedChallenge.Purpose != auth.OTPPurposeLogin {
		t.Fatalf(
			"stored purpose = %q, want %q",
			storedChallenge.Purpose,
			auth.OTPPurposeLogin,
		)
	}

	if storedChallenge.TargetIdentityID != nil {
		t.Fatal(
			"email login challenge unexpectedly targets an identity",
		)
	}
}

func TestChallengeRepositoryStoresLinkIdentifierTarget(
	t *testing.T,
) {
	ctx, pool, repository := newChallengeRepositoryIntegrationTest(t)

	const challengeID = "otp_ch_integration_link_identifier"

	identityID := createIntegrationIdentity(
		t,
		ctx,
		pool,
	)

	cleanupChallengeIDs(
		t,
		pool,
		challengeID,
	)

	challenge := auth.OTPChallenge{
		ID: challengeID,
		Identifier: auth.Identifier{
			Type:  auth.IdentifierTypeEmail,
			Value: "linked@example.com",
		},
		Purpose:          auth.OTPPurposeLinkIdentifier,
		TargetIdentityID: &identityID,
		CodeHash:         "link-identifier-code-hash",
		ExpiresAt: time.Now().
			UTC().
			Add(5 * time.Minute),
	}

	if err := repository.Create(
		ctx,
		challenge,
	); err != nil {
		t.Fatalf(
			"Create() returned an error: %v",
			err,
		)
	}

	storedChallenge, err := repository.FindByID(
		ctx,
		challengeID,
	)
	if err != nil {
		t.Fatalf(
			"FindByID() returned an error: %v",
			err,
		)
	}

	if storedChallenge.TargetIdentityID == nil {
		t.Fatal(
			"link identifier challenge has nil target identity",
		)
	}

	if *storedChallenge.TargetIdentityID != identityID {
		t.Fatalf(
			"target identity = %q, want %q",
			*storedChallenge.TargetIdentityID,
			identityID,
		)
	}

	if storedChallenge.Purpose !=
		auth.OTPPurposeLinkIdentifier {
		t.Fatalf(
			"purpose = %q, want %q",
			storedChallenge.Purpose,
			auth.OTPPurposeLinkIdentifier,
		)
	}
}

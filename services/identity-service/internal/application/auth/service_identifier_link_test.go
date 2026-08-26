package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestVerifyOTPCompletesIdentifierLinkWithoutIssuingTokens(
	t *testing.T,
) {
	now := time.Date(
		2026,
		time.August,
		13,
		10,
		30,
		0,
		0,
		time.UTC,
	)

	identityID :=
		"11111111-1111-1111-1111-111111111111"

	identifier := Identifier{
		Type:  IdentifierTypeEmail,
		Value: "linked@example.com",
	}

	challengeRepository := &testChallengeRepository{
		findResult: OTPChallenge{
			ID:               "otp_ch_link_email",
			Identifier:       identifier,
			Purpose:          OTPPurposeLinkIdentifier,
			TargetIdentityID: &identityID,
			CodeHash:         "hashed-code",
			ExpiresAt:        now.Add(5 * time.Minute),

			FailedAttempts: 0,
			MaxAttempts:    5,
		},
	}

	identityRepository :=
		&testIdentityIdentifierRepository{}

	linkStore :=
		&testIdentifierLinkCompletionStore{}

	tokenIssuer := &testTokenIssuer{}

	service := newIdentifierAwareServiceForTest(
		challengeRepository,
		identityRepository,
		&testIdentityReader{},
		linkStore,
		&testOTPHasher{},
		tokenIssuer,
		&testClock{now: now},
	)

	result, err := service.VerifyOTP(
		context.Background(),
		VerifyOTPInput{
			ExpectedPurpose:          OTPPurposeLinkIdentifier,
			ExpectedTargetIdentityID: &identityID,
			ChallengeID:              "otp_ch_link_email",
			Code:                     "123456",
		},
	)
	if err != nil {
		t.Fatalf(
			"VerifyOTP() returned an error: %v",
			err,
		)
	}

	if linkStore.calls != 1 {
		t.Fatalf(
			"IdentifierLinkCompletionStore calls = %d, expected 1",
			linkStore.calls,
		)
	}

	expectedInput := IdentifierLinkCompletionInput{
		ChallengeID: "otp_ch_link_email",
		IdentityID:  identityID,
		Identifier:  identifier,
		VerifiedAt:  now,
	}

	if linkStore.input.ChallengeID !=
		expectedInput.ChallengeID {
		t.Fatalf(
			"link challenge ID = %q, expected %q",
			linkStore.input.ChallengeID,
			expectedInput.ChallengeID,
		)
	}

	if linkStore.input.IdentityID !=
		expectedInput.IdentityID {
		t.Fatalf(
			"link identity ID = %q, expected %q",
			linkStore.input.IdentityID,
			expectedInput.IdentityID,
		)
	}

	if linkStore.input.Identifier !=
		expectedInput.Identifier {
		t.Fatalf(
			"link identifier = %+v, expected %+v",
			linkStore.input.Identifier,
			expectedInput.Identifier,
		)
	}

	if !linkStore.input.VerifiedAt.Equal(
		expectedInput.VerifiedAt,
	) {
		t.Fatalf(
			"link verification time = %v, expected %v",
			linkStore.input.VerifiedAt,
			expectedInput.VerifiedAt,
		)
	}

	if identityRepository.findCalls != 0 ||
		identityRepository.createCalls != 0 ||
		identityRepository.linkCalls != 0 {
		t.Fatal(
			"link_identifier used IdentityIdentifierRepository directly",
		)
	}

	if tokenIssuer.calls != 0 {
		t.Fatal(
			"link_identifier issued authentication tokens",
		)
	}

	if result.IdentityID != identityID {
		t.Fatalf(
			"result identity ID = %q, expected %q",
			result.IdentityID,
			identityID,
		)
	}

	if result.AccessToken != "" ||
		result.RefreshToken != "" ||
		result.AccessTokenExpiresInSeconds != 0 {
		t.Fatal(
			"link_identifier returned authentication tokens",
		)
	}
}

func TestVerifyOTPRejectsIdentifierLinkForDifferentAuthenticatedIdentity(
	t *testing.T,
) {
	now := time.Date(
		2026,
		time.August,
		13,
		11,
		0,
		0,
		0,
		time.UTC,
	)

	challengeTargetIdentityID :=
		"11111111-1111-1111-1111-111111111111"

	authenticatedIdentityID :=
		"22222222-2222-2222-2222-222222222222"

	challengeRepository := &testChallengeRepository{
		findResult: OTPChallenge{
			ID: "otp_ch_wrong_identity",
			Identifier: Identifier{
				Type:  IdentifierTypeEmail,
				Value: "linked@example.com",
			},
			Purpose:          OTPPurposeLinkIdentifier,
			TargetIdentityID: &challengeTargetIdentityID,
			CodeHash:         "hashed-code",
			ExpiresAt:        now.Add(5 * time.Minute),
			FailedAttempts:   0,
			MaxAttempts:      5,
		},
	}

	otpHasher := &testOTPHasher{}

	linkStore :=
		&testIdentifierLinkCompletionStore{}

	tokenIssuer := &testTokenIssuer{}

	service := newIdentifierAwareServiceForTest(
		challengeRepository,
		&testIdentityIdentifierRepository{},
		&testIdentityReader{},
		linkStore,
		otpHasher,
		tokenIssuer,
		&testClock{now: now},
	)

	_, err := service.VerifyOTP(
		context.Background(),
		VerifyOTPInput{
			ExpectedPurpose:          OTPPurposeLinkIdentifier,
			ExpectedTargetIdentityID: &authenticatedIdentityID,
			ChallengeID:              "otp_ch_wrong_identity",
			Code:                     "123456",
		},
	)

	if !errors.Is(
		err,
		ErrOTPChallengeTargetMismatch,
	) {
		t.Fatalf(
			"VerifyOTP() error = %v, expected %v",
			err,
			ErrOTPChallengeTargetMismatch,
		)
	}

	if otpHasher.compareCalled {
		t.Fatal(
			"OTP code was compared before target identity validation",
		)
	}

	if linkStore.calls != 0 {
		t.Fatalf(
			"IdentifierLinkCompletionStore calls = %d, expected 0",
			linkStore.calls,
		)
	}

	if tokenIssuer.calls != 0 {
		t.Fatal(
			"authentication tokens were issued for mismatched target identity",
		)
	}
}

func TestVerifyOTPMapsIdentifierAlreadyLinkedWithoutIssuingTokens(
	t *testing.T,
) {
	now := time.Date(
		2026,
		time.August,
		13,
		11,
		0,
		0,
		0,
		time.UTC,
	)

	identityID :=
		"22222222-2222-2222-2222-222222222222"

	identifier := Identifier{
		Type:  IdentifierTypePhone,
		Value: "+9647500000077",
	}

	challengeRepository := &testChallengeRepository{
		findResult: OTPChallenge{
			ID:               "otp_ch_link_conflict",
			Identifier:       identifier,
			Purpose:          OTPPurposeLinkIdentifier,
			TargetIdentityID: &identityID,
			CodeHash:         "hashed-code",
			ExpiresAt:        now.Add(5 * time.Minute),

			FailedAttempts: 0,
			MaxAttempts:    5,
		},
	}

	linkStore := &testIdentifierLinkCompletionStore{
		err: ErrIdentifierAlreadyLinked,
	}

	tokenIssuer := &testTokenIssuer{}

	service := newIdentifierAwareServiceForTest(
		challengeRepository,
		&testIdentityIdentifierRepository{},
		&testIdentityReader{},
		linkStore,
		&testOTPHasher{},
		tokenIssuer,
		&testClock{now: now},
	)

	_, err := service.VerifyOTP(
		context.Background(),
		VerifyOTPInput{
			ExpectedPurpose:          OTPPurposeLinkIdentifier,
			ExpectedTargetIdentityID: &identityID,
			ChallengeID:              "otp_ch_link_conflict",
			Code:                     "123456",
		},
	)

	if !errors.Is(
		err,
		ErrIdentifierAlreadyLinked,
	) {
		t.Fatalf(
			"VerifyOTP() error = %v, expected %v",
			err,
			ErrIdentifierAlreadyLinked,
		)
	}

	if linkStore.calls != 1 {
		t.Fatalf(
			"IdentifierLinkCompletionStore calls = %d, expected 1",
			linkStore.calls,
		)
	}

	if tokenIssuer.calls != 0 {
		t.Fatal(
			"identifier ownership conflict issued tokens",
		)
	}
}

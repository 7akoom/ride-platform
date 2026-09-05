package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestVerifyOTPCompletesIdentifierUnlinkWithoutIssuingTokens(
	t *testing.T,
) {
	now := time.Date(
		2026,
		time.August,
		26,
		10,
		0,
		0,
		0,
		time.UTC,
	)

	identityID :=
		"11111111-1111-1111-1111-111111111111"

	challengeRepository := &testChallengeRepository{
		findResult: OTPChallenge{
			ID: "otp_ch_unlink_email",
			Identifier: Identifier{
				Type:  IdentifierTypePhone,
				Value: "+9647500000601",
			},
			Purpose:          OTPPurposeUnlinkIdentifier,
			TargetIdentityID: &identityID,
			CodeHash:         "hashed-code",
			ExpiresAt:        now.Add(5 * time.Minute),
			FailedAttempts:   0,
			MaxAttempts:      5,
		},
	}

	unlinkStore :=
		&testIdentifierUnlinkCompletionStore{}

	tokenIssuer := &testTokenIssuer{}

	dependencies :=
		newValidServiceConstructorTestDependencies()

	dependencies.challengeRepository =
		challengeRepository

	dependencies.identifierUnlinkCompletionStore =
		unlinkStore

	dependencies.otpHasher =
		&testOTPHasher{}

	dependencies.tokenIssuer =
		tokenIssuer

	dependencies.clock =
		&testClock{now: now}

	service :=
		newServiceFromConstructorTestDependencies(
			dependencies,
		)

	result, err := service.VerifyOTP(
		context.Background(),
		VerifyOTPInput{
			ExpectedPurpose:          OTPPurposeUnlinkIdentifier,
			ExpectedTargetIdentityID: &identityID,
			ChallengeID:              "otp_ch_unlink_email",
			Code:                     "123456",
		},
	)
	if err != nil {
		t.Fatalf(
			"VerifyOTP() returned an error: %v",
			err,
		)
	}

	if unlinkStore.calls != 1 {
		t.Fatalf(
			"IdentifierUnlinkCompletionStore calls = %d, expected 1",
			unlinkStore.calls,
		)
	}

	if unlinkStore.input.ChallengeID !=
		"otp_ch_unlink_email" {
		t.Fatalf(
			"unlink challenge ID = %q, expected %q",
			unlinkStore.input.ChallengeID,
			"otp_ch_unlink_email",
		)
	}

	if unlinkStore.input.IdentityID != identityID {
		t.Fatalf(
			"unlink identity ID = %q, expected %q",
			unlinkStore.input.IdentityID,
			identityID,
		)
	}

	if !unlinkStore.input.VerifiedAt.Equal(now) {
		t.Fatalf(
			"unlink verification time = %v, expected %v",
			unlinkStore.input.VerifiedAt,
			now,
		)
	}

	if tokenIssuer.calls != 0 {
		t.Fatal(
			"unlink_identifier issued authentication tokens",
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
			"unlink_identifier returned authentication tokens",
		)
	}
}

func TestVerifyOTPRejectsIdentifierUnlinkForDifferentAuthenticatedIdentity(
	t *testing.T,
) {
	now := time.Date(
		2026,
		time.August,
		26,
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
			ID: "otp_ch_unlink_wrong_identity",
			Identifier: Identifier{
				Type:  IdentifierTypeEmail,
				Value: "verify-unlink@example.com",
			},
			Purpose:          OTPPurposeUnlinkIdentifier,
			TargetIdentityID: &challengeTargetIdentityID,
			CodeHash:         "hashed-code",
			ExpiresAt:        now.Add(5 * time.Minute),
			FailedAttempts:   0,
			MaxAttempts:      5,
		},
	}

	otpHasher := &testOTPHasher{}

	unlinkStore :=
		&testIdentifierUnlinkCompletionStore{}

	tokenIssuer := &testTokenIssuer{}

	dependencies :=
		newValidServiceConstructorTestDependencies()

	dependencies.challengeRepository =
		challengeRepository

	dependencies.identifierUnlinkCompletionStore =
		unlinkStore

	dependencies.otpHasher =
		otpHasher

	dependencies.tokenIssuer =
		tokenIssuer

	dependencies.clock =
		&testClock{now: now}

	service :=
		newServiceFromConstructorTestDependencies(
			dependencies,
		)

	_, err := service.VerifyOTP(
		context.Background(),
		VerifyOTPInput{
			ExpectedPurpose:          OTPPurposeUnlinkIdentifier,
			ExpectedTargetIdentityID: &authenticatedIdentityID,
			ChallengeID:              "otp_ch_unlink_wrong_identity",
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
			"OTP code was compared before unlink target identity validation",
		)
	}

	if unlinkStore.calls != 0 {
		t.Fatalf(
			"IdentifierUnlinkCompletionStore calls = %d, expected 0",
			unlinkStore.calls,
		)
	}

	if tokenIssuer.calls != 0 {
		t.Fatal(
			"authentication tokens were issued for mismatched unlink identity",
		)
	}
}

func TestVerifyOTPMapsIdentifierUnlinkCompletionDomainErrors(
	t *testing.T,
) {
	tests := []struct {
		name string
		err  error
	}{
		{
			name: "identity not found",
			err:  ErrIdentityNotFound,
		},
		{
			name: "identifier not linked",
			err:  ErrIdentifierNotLinked,
		},
		{
			name: "last identifier removal",
			err:  ErrLastIdentifierRemoval,
		},
		{
			name: "challenge not found",
			err:  ErrChallengeNotFound,
		},
		{
			name: "challenge expired",
			err:  ErrChallengeExpired,
		},
		{
			name: "challenge used",
			err:  ErrChallengeUsed,
		},
		{
			name: "challenge cancelled",
			err:  ErrChallengeCancelled,
		},
		{
			name: "challenge attempts exceeded",
			err:  ErrChallengeAttemptsExceeded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Date(
				2026,
				time.August,
				26,
				12,
				0,
				0,
				0,
				time.UTC,
			)

			identityID :=
				"33333333-3333-3333-3333-333333333333"

			challengeRepository :=
				&testChallengeRepository{
					findResult: OTPChallenge{
						ID: "otp_ch_unlink_domain_error",
						Identifier: Identifier{
							Type:  IdentifierTypeEmail,
							Value: "unlink-domain@example.com",
						},
						Purpose:          OTPPurposeUnlinkIdentifier,
						TargetIdentityID: &identityID,
						CodeHash:         "hashed-code",
						ExpiresAt:        now.Add(5 * time.Minute),
						FailedAttempts:   0,
						MaxAttempts:      5,
					},
				}

			unlinkStore :=
				&testIdentifierUnlinkCompletionStore{
					err: tt.err,
				}

			tokenIssuer := &testTokenIssuer{}

			dependencies :=
				newValidServiceConstructorTestDependencies()

			dependencies.challengeRepository =
				challengeRepository

			dependencies.identifierUnlinkCompletionStore =
				unlinkStore

			dependencies.otpHasher =
				&testOTPHasher{}

			dependencies.tokenIssuer =
				tokenIssuer

			dependencies.clock =
				&testClock{now: now}

			service :=
				newServiceFromConstructorTestDependencies(
					dependencies,
				)

			_, err := service.VerifyOTP(
				context.Background(),
				VerifyOTPInput{
					ExpectedPurpose:          OTPPurposeUnlinkIdentifier,
					ExpectedTargetIdentityID: &identityID,
					ChallengeID:              "otp_ch_unlink_domain_error",
					Code:                     "123456",
				},
			)

			if !errors.Is(err, tt.err) {
				t.Fatalf(
					"VerifyOTP() error = %v, expected %v",
					err,
					tt.err,
				)
			}

			if unlinkStore.calls != 1 {
				t.Fatalf(
					"IdentifierUnlinkCompletionStore calls = %d, expected 1",
					unlinkStore.calls,
				)
			}

			if tokenIssuer.calls != 0 {
				t.Fatal(
					"identifier unlink completion error issued authentication tokens",
				)
			}
		})
	}
}

func TestVerifyOTPWrapsUnexpectedIdentifierUnlinkCompletionError(
	t *testing.T,
) {
	now := time.Date(
		2026,
		time.August,
		26,
		13,
		0,
		0,
		0,
		time.UTC,
	)

	identityID :=
		"44444444-4444-4444-4444-444444444444"

	completionError := errors.New(
		"identifier unlink transaction failed",
	)

	challengeRepository := &testChallengeRepository{
		findResult: OTPChallenge{
			ID: "otp_ch_unlink_unexpected_error",
			Identifier: Identifier{
				Type:  IdentifierTypePhone,
				Value: "+9647500000602",
			},
			Purpose:          OTPPurposeUnlinkIdentifier,
			TargetIdentityID: &identityID,
			CodeHash:         "hashed-code",
			ExpiresAt:        now.Add(5 * time.Minute),
			FailedAttempts:   0,
			MaxAttempts:      5,
		},
	}

	unlinkStore :=
		&testIdentifierUnlinkCompletionStore{
			err: completionError,
		}

	dependencies :=
		newValidServiceConstructorTestDependencies()

	dependencies.challengeRepository =
		challengeRepository

	dependencies.identifierUnlinkCompletionStore =
		unlinkStore

	dependencies.otpHasher =
		&testOTPHasher{}

	dependencies.clock =
		&testClock{now: now}

	service :=
		newServiceFromConstructorTestDependencies(
			dependencies,
		)

	_, err := service.VerifyOTP(
		context.Background(),
		VerifyOTPInput{
			ExpectedPurpose:          OTPPurposeUnlinkIdentifier,
			ExpectedTargetIdentityID: &identityID,
			ChallengeID:              "otp_ch_unlink_unexpected_error",
			Code:                     "123456",
		},
	)

	if !errors.Is(
		err,
		completionError,
	) {
		t.Fatalf(
			"VerifyOTP() error = %v, expected wrapped %v",
			err,
			completionError,
		)
	}

	if unlinkStore.calls != 1 {
		t.Fatalf(
			"IdentifierUnlinkCompletionStore calls = %d, expected 1",
			unlinkStore.calls,
		)
	}
}

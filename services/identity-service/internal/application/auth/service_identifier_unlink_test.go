package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRequestIdentifierUnlinkOTPCreatesRequestAndDeliversToAnotherIdentifier(
	t *testing.T,
) {
	targetIdentifier := Identifier{
		Type:  IdentifierTypeEmail,
		Value: "target.unlink@example.com",
	}

	verificationIdentifier := Identifier{
		Type:  IdentifierTypePhone,
		Value: "+9647500000401",
	}

	identityReader := &testIdentityReader{
		findResult: IdentityDetails{
			ID: "identity-123",
			Identifiers: []IdentityDetailsIdentifier{
				{
					Identifier: targetIdentifier,
				},
				{
					Identifier: verificationIdentifier,
				},
				{
					Identifier: Identifier{
						Type:  IdentifierTypeEmail,
						Value: "second-verification@example.com",
					},
				},
			},
		},
		findFound: true,
	}

	unlinkStore := &testIdentifierUnlinkRequestStore{}
	otpDelivery := &testOTPDelivery{}
	rateLimiter := &testOTPRequestRateLimiter{}

	dependencies :=
		newValidServiceConstructorTestDependencies()

	dependencies.identityReader = identityReader
	dependencies.identifierUnlinkRequestStore = unlinkStore
	dependencies.otpDelivery = otpDelivery
	dependencies.otpRequestRateLimiter = rateLimiter

	service :=
		newServiceFromConstructorTestDependencies(
			dependencies,
		)

	result, err := service.RequestIdentifierUnlinkOTP(
		context.Background(),
		RequestIdentifierUnlinkOTPInput{
			IdentityID: " identity-123 ",
			TargetIdentifier: Identifier{
				Type:  IdentifierTypeEmail,
				Value: "TARGET.UNLINK@EXAMPLE.COM",
			},
		},
	)
	if err != nil {
		t.Fatalf(
			"RequestIdentifierUnlinkOTP() error = %v",
			err,
		)
	}

	if identityReader.findCalls != 1 {
		t.Fatalf(
			"identity reader calls = %d, expected 1",
			identityReader.findCalls,
		)
	}

	if identityReader.findIdentityID != "identity-123" {
		t.Fatalf(
			"identity reader identity ID = %q, expected %q",
			identityReader.findIdentityID,
			"identity-123",
		)
	}

	if !rateLimiter.called {
		t.Fatal(
			"OTP request rate limiter was not called",
		)
	}

	if rateLimiter.scope.Identifier !=
		verificationIdentifier {
		t.Fatalf(
			"rate limiter identifier = %#v, expected %#v",
			rateLimiter.scope.Identifier,
			verificationIdentifier,
		)
	}

	if rateLimiter.scope.Purpose !=
		OTPPurposeUnlinkIdentifier {
		t.Fatalf(
			"rate limiter purpose = %q, expected %q",
			rateLimiter.scope.Purpose,
			OTPPurposeUnlinkIdentifier,
		)
	}

	if rateLimiter.scope.TargetIdentityID == nil {
		t.Fatal(
			"rate limiter target identity ID is nil",
		)
	}

	if *rateLimiter.scope.TargetIdentityID !=
		"identity-123" {
		t.Fatalf(
			"rate limiter target identity ID = %q, expected %q",
			*rateLimiter.scope.TargetIdentityID,
			"identity-123",
		)
	}

	if unlinkStore.calls != 1 {
		t.Fatalf(
			"unlink request store calls = %d, expected 1",
			unlinkStore.calls,
		)
	}

	if unlinkStore.input.TargetIdentifier !=
		targetIdentifier {
		t.Fatalf(
			"unlink target identifier = %#v, expected %#v",
			unlinkStore.input.TargetIdentifier,
			targetIdentifier,
		)
	}

	challenge := unlinkStore.input.Challenge

	if challenge.ID != "otp_ch_test" {
		t.Fatalf(
			"challenge ID = %q, expected %q",
			challenge.ID,
			"otp_ch_test",
		)
	}

	if challenge.Identifier !=
		verificationIdentifier {
		t.Fatalf(
			"challenge verification identifier = %#v, expected %#v",
			challenge.Identifier,
			verificationIdentifier,
		)
	}

	if challenge.Purpose !=
		OTPPurposeUnlinkIdentifier {
		t.Fatalf(
			"challenge purpose = %q, expected %q",
			challenge.Purpose,
			OTPPurposeUnlinkIdentifier,
		)
	}

	if challenge.TargetIdentityID == nil {
		t.Fatal(
			"challenge target identity ID is nil",
		)
	}

	if *challenge.TargetIdentityID !=
		"identity-123" {
		t.Fatalf(
			"challenge target identity ID = %q, expected %q",
			*challenge.TargetIdentityID,
			"identity-123",
		)
	}

	if !otpDelivery.called {
		t.Fatal(
			"OTP delivery was not called",
		)
	}

	if otpDelivery.recipient != verificationIdentifier {
		t.Fatalf(
			"OTP recipient = %+v, expected %+v",
			otpDelivery.recipient,
			verificationIdentifier,
		)
	}

	if otpDelivery.recipient == targetIdentifier {
		t.Fatal(
			"OTP was delivered to the identifier being removed",
		)
	}

	if otpDelivery.code != "123456" {
		t.Fatalf(
			"OTP code = %q, expected %q",
			otpDelivery.code,
			"123456",
		)
	}

	if result.ChallengeID != "otp_ch_test" {
		t.Fatalf(
			"ChallengeID = %q, expected %q",
			result.ChallengeID,
			"otp_ch_test",
		)
	}

	if result.ExpiresInSeconds != 300 {
		t.Fatalf(
			"ExpiresInSeconds = %d, expected 300",
			result.ExpiresInSeconds,
		)
	}
}

func TestRequestIdentifierUnlinkOTPRejectsIdentifierNotLinked(
	t *testing.T,
) {
	identityReader := &testIdentityReader{
		findResult: IdentityDetails{
			ID: "identity-123",
			Identifiers: []IdentityDetailsIdentifier{
				{
					Identifier: Identifier{
						Type:  IdentifierTypePhone,
						Value: "+9647500000402",
					},
				},
				{
					Identifier: Identifier{
						Type:  IdentifierTypeEmail,
						Value: "owned@example.com",
					},
				},
			},
		},
		findFound: true,
	}

	unlinkStore := &testIdentifierUnlinkRequestStore{}
	otpDelivery := &testOTPDelivery{}
	rateLimiter := &testOTPRequestRateLimiter{}

	dependencies :=
		newValidServiceConstructorTestDependencies()

	dependencies.identityReader = identityReader
	dependencies.identifierUnlinkRequestStore = unlinkStore
	dependencies.otpDelivery = otpDelivery
	dependencies.otpRequestRateLimiter = rateLimiter

	service :=
		newServiceFromConstructorTestDependencies(
			dependencies,
		)

	_, err := service.RequestIdentifierUnlinkOTP(
		context.Background(),
		RequestIdentifierUnlinkOTPInput{
			IdentityID: "identity-123",
			TargetIdentifier: Identifier{
				Type:  IdentifierTypeEmail,
				Value: "missing@example.com",
			},
		},
	)

	if !errors.Is(
		err,
		ErrIdentifierNotLinked,
	) {
		t.Fatalf(
			"error = %v, expected %v",
			err,
			ErrIdentifierNotLinked,
		)
	}

	if rateLimiter.called {
		t.Fatal(
			"rate limiter was called for an unlinked target identifier",
		)
	}

	if unlinkStore.calls != 0 {
		t.Fatalf(
			"unlink store calls = %d, expected 0",
			unlinkStore.calls,
		)
	}

	if otpDelivery.called {
		t.Fatal(
			"OTP delivery was called for an unlinked target identifier",
		)
	}
}

func TestRequestIdentifierUnlinkOTPRejectsLastIdentifierRemoval(
	t *testing.T,
) {
	targetIdentifier := Identifier{
		Type:  IdentifierTypePhone,
		Value: "+9647500000403",
	}

	identityReader := &testIdentityReader{
		findResult: IdentityDetails{
			ID: "identity-123",
			Identifiers: []IdentityDetailsIdentifier{
				{
					Identifier: targetIdentifier,
				},
			},
		},
		findFound: true,
	}

	unlinkStore := &testIdentifierUnlinkRequestStore{}
	otpDelivery := &testOTPDelivery{}
	rateLimiter := &testOTPRequestRateLimiter{}

	dependencies :=
		newValidServiceConstructorTestDependencies()

	dependencies.identityReader = identityReader
	dependencies.identifierUnlinkRequestStore = unlinkStore
	dependencies.otpDelivery = otpDelivery
	dependencies.otpRequestRateLimiter = rateLimiter

	service :=
		newServiceFromConstructorTestDependencies(
			dependencies,
		)

	_, err := service.RequestIdentifierUnlinkOTP(
		context.Background(),
		RequestIdentifierUnlinkOTPInput{
			IdentityID:       "identity-123",
			TargetIdentifier: targetIdentifier,
		},
	)

	if !errors.Is(
		err,
		ErrLastIdentifierRemoval,
	) {
		t.Fatalf(
			"error = %v, expected %v",
			err,
			ErrLastIdentifierRemoval,
		)
	}

	if rateLimiter.called {
		t.Fatal(
			"rate limiter was called for last identifier removal",
		)
	}

	if unlinkStore.calls != 0 {
		t.Fatalf(
			"unlink store calls = %d, expected 0",
			unlinkStore.calls,
		)
	}

	if otpDelivery.called {
		t.Fatal(
			"OTP delivery was called for last identifier removal",
		)
	}
}

func TestRequestIdentifierUnlinkOTPStopsWhenRateLimited(
	t *testing.T,
) {
	targetIdentifier := Identifier{
		Type:  IdentifierTypePhone,
		Value: "+9647500000404",
	}

	verificationIdentifier := Identifier{
		Type:  IdentifierTypeEmail,
		Value: "unlink-rate-limit@example.com",
	}

	identityReader := &testIdentityReader{
		findResult: IdentityDetails{
			ID: "identity-123",
			Identifiers: []IdentityDetailsIdentifier{
				{
					Identifier: targetIdentifier,
				},
				{
					Identifier: verificationIdentifier,
				},
			},
		},
		findFound: true,
	}

	unlinkStore := &testIdentifierUnlinkRequestStore{}
	otpDelivery := &testOTPDelivery{}
	otpGenerator := &testOTPGenerator{}

	rateLimiter := &testOTPRequestRateLimiter{
		err: ErrOTPRequestRateLimited,
	}

	dependencies :=
		newValidServiceConstructorTestDependencies()

	dependencies.identityReader = identityReader
	dependencies.identifierUnlinkRequestStore = unlinkStore
	dependencies.otpDelivery = otpDelivery
	dependencies.otpGenerator = otpGenerator
	dependencies.otpRequestRateLimiter = rateLimiter

	service :=
		newServiceFromConstructorTestDependencies(
			dependencies,
		)

	_, err := service.RequestIdentifierUnlinkOTP(
		context.Background(),
		RequestIdentifierUnlinkOTPInput{
			IdentityID:       "identity-123",
			TargetIdentifier: targetIdentifier,
		},
	)

	if !errors.Is(
		err,
		ErrOTPRequestRateLimited,
	) {
		t.Fatalf(
			"error = %v, expected %v",
			err,
			ErrOTPRequestRateLimited,
		)
	}

	if !rateLimiter.called {
		t.Fatal(
			"rate limiter was not called",
		)
	}

	if otpGenerator.called {
		t.Fatal(
			"OTP was generated after request was rate limited",
		)
	}

	if unlinkStore.calls != 0 {
		t.Fatalf(
			"unlink store calls = %d, expected 0",
			unlinkStore.calls,
		)
	}

	if otpDelivery.called {
		t.Fatal(
			"OTP delivery was called after request was rate limited",
		)
	}
}

func TestRequestIdentifierUnlinkOTPPropagatesAtomicStoreDomainErrors(
	t *testing.T,
) {
	tests := []struct {
		name string
		err  error
	}{
		{
			name: "identity disappeared",
			err:  ErrIdentityNotFound,
		},
		{
			name: "target identifier disappeared",
			err:  ErrIdentifierNotLinked,
		},
		{
			name: "target became last identifier",
			err:  ErrLastIdentifierRemoval,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			targetIdentifier := Identifier{
				Type:  IdentifierTypePhone,
				Value: "+9647500000405",
			}

			verificationIdentifier := Identifier{
				Type:  IdentifierTypeEmail,
				Value: "unlink-store-domain@example.com",
			}

			identityReader := &testIdentityReader{
				findResult: IdentityDetails{
					ID: "identity-123",
					Identifiers: []IdentityDetailsIdentifier{
						{
							Identifier: targetIdentifier,
						},
						{
							Identifier: verificationIdentifier,
						},
					},
				},
				findFound: true,
			}

			unlinkStore :=
				&testIdentifierUnlinkRequestStore{
					err: tt.err,
				}

			otpDelivery := &testOTPDelivery{}

			dependencies :=
				newValidServiceConstructorTestDependencies()

			dependencies.identityReader =
				identityReader

			dependencies.identifierUnlinkRequestStore =
				unlinkStore

			dependencies.otpDelivery =
				otpDelivery

			service :=
				newServiceFromConstructorTestDependencies(
					dependencies,
				)

			_, err :=
				service.RequestIdentifierUnlinkOTP(
					context.Background(),
					RequestIdentifierUnlinkOTPInput{
						IdentityID:       "identity-123",
						TargetIdentifier: targetIdentifier,
					},
				)

			if !errors.Is(err, tt.err) {
				t.Fatalf(
					"error = %v, expected %v",
					err,
					tt.err,
				)
			}

			if unlinkStore.calls != 1 {
				t.Fatalf(
					"unlink store calls = %d, expected 1",
					unlinkStore.calls,
				)
			}

			if otpDelivery.called {
				t.Fatal(
					"OTP delivery was called after unlink store failure",
				)
			}
		})
	}
}

func TestRequestIdentifierUnlinkOTPWrapsUnexpectedStoreError(
	t *testing.T,
) {
	storeError := errors.New(
		"unlink request transaction failed",
	)

	targetIdentifier := Identifier{
		Type:  IdentifierTypePhone,
		Value: "+9647500000406",
	}

	verificationIdentifier := Identifier{
		Type:  IdentifierTypeEmail,
		Value: "unlink-store-error@example.com",
	}

	identityReader := &testIdentityReader{
		findResult: IdentityDetails{
			ID: "identity-123",
			Identifiers: []IdentityDetailsIdentifier{
				{
					Identifier: targetIdentifier,
				},
				{
					Identifier: verificationIdentifier,
				},
			},
		},
		findFound: true,
	}

	unlinkStore := &testIdentifierUnlinkRequestStore{
		err: storeError,
	}

	otpDelivery := &testOTPDelivery{}

	dependencies :=
		newValidServiceConstructorTestDependencies()

	dependencies.identityReader = identityReader
	dependencies.identifierUnlinkRequestStore = unlinkStore
	dependencies.otpDelivery = otpDelivery

	service :=
		newServiceFromConstructorTestDependencies(
			dependencies,
		)

	_, err := service.RequestIdentifierUnlinkOTP(
		context.Background(),
		RequestIdentifierUnlinkOTPInput{
			IdentityID:       "identity-123",
			TargetIdentifier: targetIdentifier,
		},
	)

	if !errors.Is(err, storeError) {
		t.Fatalf(
			"error = %v, expected wrapped %v",
			err,
			storeError,
		)
	}

	if otpDelivery.called {
		t.Fatal(
			"OTP delivery was called after unlink store error",
		)
	}
}

func TestRequestIdentifierUnlinkOTPCancelsChallengeWhenDeliveryFails(
	t *testing.T,
) {
	deliveryError := errors.New(
		"OTP provider unavailable",
	)

	cancellationError := errors.New(
		"challenge cancellation failed",
	)

	targetIdentifier := Identifier{
		Type:  IdentifierTypePhone,
		Value: "+9647500000407",
	}

	verificationIdentifier := Identifier{
		Type:  IdentifierTypeEmail,
		Value: "unlink-delivery-error@example.com",
	}

	identityReader := &testIdentityReader{
		findResult: IdentityDetails{
			ID: "identity-123",
			Identifiers: []IdentityDetailsIdentifier{
				{
					Identifier: targetIdentifier,
				},
				{
					Identifier: verificationIdentifier,
				},
			},
		},
		findFound: true,
	}

	unlinkStore := &testIdentifierUnlinkRequestStore{}

	otpDelivery := &testOTPDelivery{
		err: deliveryError,
	}

	challengeRepository := &testChallengeRepository{
		cancelErr: cancellationError,
	}

	dependencies :=
		newValidServiceConstructorTestDependencies()

	dependencies.challengeRepository =
		challengeRepository

	dependencies.identityReader =
		identityReader

	dependencies.identifierUnlinkRequestStore =
		unlinkStore

	dependencies.otpDelivery =
		otpDelivery

	service :=
		newServiceFromConstructorTestDependencies(
			dependencies,
		)

	_, err := service.RequestIdentifierUnlinkOTP(
		context.Background(),
		RequestIdentifierUnlinkOTPInput{
			IdentityID:       "identity-123",
			TargetIdentifier: targetIdentifier,
		},
	)

	if !errors.Is(err, deliveryError) {
		t.Fatalf(
			"error = %v, expected delivery error %v",
			err,
			deliveryError,
		)
	}

	if !errors.Is(err, cancellationError) {
		t.Fatalf(
			"error = %v, expected cancellation error %v",
			err,
			cancellationError,
		)
	}

	if unlinkStore.calls != 1 {
		t.Fatalf(
			"unlink store calls = %d, expected 1",
			unlinkStore.calls,
		)
	}

	if !otpDelivery.called {
		t.Fatal(
			"OTP delivery was not called",
		)
	}
}
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

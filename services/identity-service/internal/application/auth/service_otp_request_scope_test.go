package auth

import (
	"context"
	"testing"
	"time"
)

func TestRequestOTPUsesGenericPhoneLoginIdentifier(
	t *testing.T,
) {
	dependencies := newValidServiceConstructorTestDependencies()

	challengeRepository := &testChallengeRepository{}
	otpDelivery := &testOTPDelivery{}
	rateLimiter := &testOTPRequestRateLimiter{}

	dependencies.challengeRepository = challengeRepository
	dependencies.otpDelivery = otpDelivery
	dependencies.otpRequestRateLimiter = rateLimiter
	dependencies.clock = &testClock{
		now: time.Date(
			2026,
			time.August,
			13,
			8,
			0,
			0,
			0,
			time.UTC,
		),
	}

	service := newServiceFromConstructorTestDependencies(
		dependencies,
	)

	_, err := service.RequestOTP(
		context.Background(),
		RequestOTPInput{
			Identifier: Identifier{
				Type:  IdentifierTypePhone,
				Value: "  +9647501234567  ",
			},
			Purpose: OTPPurposeLogin,
			Locale:  "ar",
		},
	)
	if err != nil {
		t.Fatalf(
			"RequestOTP() returned an error: %v",
			err,
		)
	}

	expectedIdentifier := Identifier{
		Type:  IdentifierTypePhone,
		Value: "+9647501234567",
	}

	if challengeRepository.createdChallenge.Identifier !=
		expectedIdentifier {
		t.Fatalf(
			"challenge identifier = %+v, expected %+v",
			challengeRepository.createdChallenge.Identifier,
			expectedIdentifier,
		)
	}

	if challengeRepository.createdChallenge.Purpose !=
		OTPPurposeLogin {
		t.Fatalf(
			"challenge purpose = %q, expected %q",
			challengeRepository.createdChallenge.Purpose,
			OTPPurposeLogin,
		)
	}

	if challengeRepository.createdChallenge.TargetIdentityID != nil {
		t.Fatal(
			"login challenge unexpectedly has target identity",
		)
	}

	if rateLimiter.identifierValue !=
		expectedIdentifier.Value {
		t.Fatalf(
			"rate limiter identifier = %q, expected %q",
			rateLimiter.identifierValue,
			expectedIdentifier.Value,
		)
	}

	if otpDelivery.recipient != expectedIdentifier {
		t.Fatalf(
			"OTP delivery recipient = %+v, expected %+v",
			otpDelivery.recipient,
			expectedIdentifier,
		)
	}

	if otpDelivery.code != "123456" {
		t.Fatalf(
			"OTP delivery code = %q, expected %q",
			otpDelivery.code,
			"123456",
		)
	}
	if otpDelivery.purpose != OTPPurposeLogin {
		t.Fatalf(
			"OTP delivery purpose = %q, expected %q",
			otpDelivery.purpose,
			OTPPurposeLogin,
		)
	}
	if otpDelivery.locale != "ar" {
		t.Fatalf(
			"OTP delivery locale = %q, expected %q",
			otpDelivery.locale,
			"ar",
		)
	}
}

func TestRequestOTPUsesNormalizedEmailLoginIdentifier(
	t *testing.T,
) {
	dependencies := newValidServiceConstructorTestDependencies()

	challengeRepository := &testChallengeRepository{}
	otpDelivery := &testOTPDelivery{}
	rateLimiter := &testOTPRequestRateLimiter{}

	dependencies.challengeRepository = challengeRepository
	dependencies.otpDelivery = otpDelivery
	dependencies.otpRequestRateLimiter = rateLimiter

	service := newServiceFromConstructorTestDependencies(
		dependencies,
	)

	_, err := service.RequestOTP(
		context.Background(),
		RequestOTPInput{
			Identifier: Identifier{
				Type:  IdentifierTypeEmail,
				Value: "  User.Name@EXAMPLE.COM  ",
			},
			Purpose: OTPPurposeLogin,
		},
	)
	if err != nil {
		t.Fatalf(
			"RequestOTP() returned an error: %v",
			err,
		)
	}

	expectedIdentifier := Identifier{
		Type:  IdentifierTypeEmail,
		Value: "user.name@example.com",
	}

	if challengeRepository.createdChallenge.Identifier !=
		expectedIdentifier {
		t.Fatalf(
			"challenge identifier = %+v, expected %+v",
			challengeRepository.createdChallenge.Identifier,
			expectedIdentifier,
		)
	}

	if challengeRepository.createdChallenge.Purpose !=
		OTPPurposeLogin {
		t.Fatalf(
			"challenge purpose = %q, expected %q",
			challengeRepository.createdChallenge.Purpose,
			OTPPurposeLogin,
		)
	}

	if challengeRepository.createdChallenge.TargetIdentityID != nil {
		t.Fatal(
			"email login challenge unexpectedly has target identity",
		)
	}

	if rateLimiter.identifierValue !=
		expectedIdentifier.Value {
		t.Fatalf(
			"rate limiter identifier = %q, expected %q",
			rateLimiter.identifierValue,
			expectedIdentifier.Value,
		)
	}

	if otpDelivery.recipient != expectedIdentifier {
		t.Fatalf(
			"OTP delivery recipient = %+v, expected %+v",
			otpDelivery.recipient,
			expectedIdentifier,
		)
	}
}

func TestRequestOTPUsesLinkIdentifierScope(
	t *testing.T,
) {
	dependencies := newValidServiceConstructorTestDependencies()

	challengeRepository := &testChallengeRepository{}
	otpDelivery := &testOTPDelivery{}

	dependencies.challengeRepository =
		challengeRepository
	dependencies.otpDelivery =
		otpDelivery

	service := newServiceFromConstructorTestDependencies(
		dependencies,
	)

	targetIdentityID :=
		"  11111111-1111-1111-1111-111111111111  "

	_, err := service.RequestOTP(
		context.Background(),
		RequestOTPInput{
			Identifier: Identifier{
				Type:  IdentifierTypeEmail,
				Value: "Link.Me@EXAMPLE.COM",
			},
			Purpose:          OTPPurposeLinkIdentifier,
			TargetIdentityID: &targetIdentityID,
			Locale:           "ku",
		},
	)
	if err != nil {
		t.Fatalf(
			"RequestOTP() returned an error: %v",
			err,
		)
	}

	challenge :=
		challengeRepository.createdChallenge

	expectedIdentifier := Identifier{
		Type:  IdentifierTypeEmail,
		Value: "link.me@example.com",
	}

	if challenge.Identifier != expectedIdentifier {
		t.Fatalf(
			"challenge identifier = %+v, expected %+v",
			challenge.Identifier,
			expectedIdentifier,
		)
	}

	if challenge.Purpose !=
		OTPPurposeLinkIdentifier {
		t.Fatalf(
			"challenge purpose = %q, expected %q",
			challenge.Purpose,
			OTPPurposeLinkIdentifier,
		)
	}

	if challenge.TargetIdentityID == nil {
		t.Fatal(
			"link identifier challenge has nil target identity",
		)
	}

	expectedIdentityID :=
		"11111111-1111-1111-1111-111111111111"

	if *challenge.TargetIdentityID !=
		expectedIdentityID {
		t.Fatalf(
			"target identity ID = %q, expected %q",
			*challenge.TargetIdentityID,
			expectedIdentityID,
		)
	}
	if !otpDelivery.called {
		t.Fatal(
			"OTP delivery was not called",
		)
	}

	if otpDelivery.purpose !=
		OTPPurposeLinkIdentifier {
		t.Fatalf(
			"OTP delivery purpose = %q, expected %q",
			otpDelivery.purpose,
			OTPPurposeLinkIdentifier,
		)
	}

	if otpDelivery.recipient != expectedIdentifier {
		t.Fatalf(
			"OTP delivery recipient = %+v, expected %+v",
			otpDelivery.recipient,
			expectedIdentifier,
		)
	}
	if otpDelivery.locale != "ku" {
		t.Fatalf(
			"OTP delivery locale = %q, expected %q",
			otpDelivery.locale,
			"ku",
		)
	}
}

func TestRequestOTPRejectsInvalidGenericScopeBeforeSideEffects(
	t *testing.T,
) {
	targetIdentityID :=
		"11111111-1111-1111-1111-111111111111"

	tests := []struct {
		name  string
		input RequestOTPInput
	}{
		{
			name: "blank generic identifier value",
			input: RequestOTPInput{
				Identifier: Identifier{
					Type:  IdentifierTypeEmail,
					Value: "   ",
				},
				Purpose: OTPPurposeLogin,
			},
		},
		{
			name: "login cannot target identity",
			input: RequestOTPInput{
				Identifier: Identifier{
					Type:  IdentifierTypeEmail,
					Value: "user@example.com",
				},
				Purpose:          OTPPurposeLogin,
				TargetIdentityID: &targetIdentityID,
			},
		},
		{
			name: "link identifier requires target identity",
			input: RequestOTPInput{
				Identifier: Identifier{
					Type:  IdentifierTypeEmail,
					Value: "user@example.com",
				},
				Purpose: OTPPurposeLinkIdentifier,
			},
		},
		{
			name: "generic request requires valid purpose",
			input: RequestOTPInput{
				Identifier: Identifier{
					Type:  IdentifierTypeEmail,
					Value: "user@example.com",
				},
			},
		},
		{
			name: "invalid purpose",
			input: RequestOTPInput{
				Identifier: Identifier{
					Type:  IdentifierTypeEmail,
					Value: "user@example.com",
				},
				Purpose: OTPPurpose("password_reset"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dependencies :=
				newValidServiceConstructorTestDependencies()

			challengeRepository :=
				&testChallengeRepository{}

			otpGenerator := &testOTPGenerator{}
			otpDelivery := &testOTPDelivery{}
			rateLimiter :=
				&testOTPRequestRateLimiter{}

			dependencies.challengeRepository =
				challengeRepository
			dependencies.otpGenerator =
				otpGenerator
			dependencies.otpDelivery =
				otpDelivery
			dependencies.otpRequestRateLimiter =
				rateLimiter

			service :=
				newServiceFromConstructorTestDependencies(
					dependencies,
				)

			_, err := service.RequestOTP(
				context.Background(),
				tt.input,
			)

			if err == nil {
				t.Fatal(
					"RequestOTP() accepted invalid generic OTP scope",
				)
			}

			if rateLimiter.called {
				t.Fatal(
					"rate limiter was called for invalid OTP request",
				)
			}

			if otpGenerator.called {
				t.Fatal(
					"OTP generator was called for invalid OTP request",
				)
			}

			if challengeRepository.createCalled {
				t.Fatal(
					"challenge was created for invalid OTP request",
				)
			}

			if otpDelivery.called {
				t.Fatal(
					"OTP delivery was called for invalid OTP request",
				)
			}
		})
	}
}

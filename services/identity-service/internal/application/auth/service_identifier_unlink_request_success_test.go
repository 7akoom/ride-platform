package auth

import (
	"context"
	"testing"
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

	if otpDelivery.purpose !=
		OTPPurposeUnlinkIdentifier {
		t.Fatalf(
			"OTP delivery purpose = %q, expected %q",
			otpDelivery.purpose,
			OTPPurposeUnlinkIdentifier,
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

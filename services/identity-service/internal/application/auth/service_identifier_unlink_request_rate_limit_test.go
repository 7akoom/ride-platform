package auth

import (
	"context"
	"errors"
	"testing"
)

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

package auth

import (
	"context"
	"errors"
	"testing"
)

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

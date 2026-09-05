package auth

import (
	"context"
	"errors"
	"testing"
)

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

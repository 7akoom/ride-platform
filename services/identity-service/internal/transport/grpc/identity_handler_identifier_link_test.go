package grpc

import (
	"context"
	"testing"

	identityv1 "github.com/7akoom/ride-platform/gen/go/ride/identity/v1"
	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestIdentityHandlerRequestIdentifierLinkOTPUsesAuthenticatedIdentity(
	t *testing.T,
) {
	authService := &fakeAuthService{
		requestOTPResult: auth.RequestOTPResult{
			ChallengeID:      "otp_ch_link_test",
			ExpiresInSeconds: 300,
		},
	}

	handler := NewIdentityHandler(authService)

	ctx := metadata.NewIncomingContext(
		context.Background(),
		metadata.Pairs(
			"accept-language",
			"ku-IQ",
		),
	)

	ctx = contextWithAuthenticatedPrincipal(
		ctx,
		authenticatedPrincipal{
			IdentityID: "identity-123",
			SessionID:  "session-456",
		},
	)

	response, err := handler.RequestIdentifierLinkOTP(
		ctx,
		&identityv1.RequestIdentifierLinkOTPRequest{
			Identifier: &identityv1.Identifier{
				Type:  identityv1.IdentifierType_IDENTIFIER_TYPE_EMAIL,
				Value: "  user@example.com  ",
			},
		},
	)
	if err != nil {
		t.Fatalf(
			"RequestIdentifierLinkOTP() returned an error: %v",
			err,
		)
	}

	if !authService.requestOTPCalled {
		t.Fatal(
			"auth service RequestOTP() was not called",
		)
	}

	expectedIdentifier := auth.Identifier{
		Type:  auth.IdentifierTypeEmail,
		Value: "user@example.com",
	}

	if authService.requestOTPInput.Identifier != expectedIdentifier {
		t.Fatalf(
			"auth service received identifier %+v, expected %+v",
			authService.requestOTPInput.Identifier,
			expectedIdentifier,
		)
	}

	if authService.requestOTPInput.Purpose !=
		auth.OTPPurposeLinkIdentifier {
		t.Fatalf(
			"auth service received purpose %q, expected %q",
			authService.requestOTPInput.Purpose,
			auth.OTPPurposeLinkIdentifier,
		)
	}

	if authService.requestOTPInput.TargetIdentityID == nil {
		t.Fatal(
			"auth service received nil target identity ID",
		)
	}

	if *authService.requestOTPInput.TargetIdentityID !=
		"identity-123" {
		t.Fatalf(
			"target identity ID = %q, expected %q",
			*authService.requestOTPInput.TargetIdentityID,
			"identity-123",
		)
	}

	if authService.requestOTPInput.Locale != "ku" {
		t.Fatalf(
			"auth service received locale %q, expected %q",
			authService.requestOTPInput.Locale,
			"ku",
		)
	}

	if response.GetChallengeId() != "otp_ch_link_test" {
		t.Fatalf(
			"ChallengeId is %q, expected %q",
			response.GetChallengeId(),
			"otp_ch_link_test",
		)
	}

	if response.GetExpiresInSeconds() != 300 {
		t.Fatalf(
			"ExpiresInSeconds is %d, expected 300",
			response.GetExpiresInSeconds(),
		)
	}
}

func TestIdentityHandlerRequestIdentifierLinkOTPRejectsMissingAuthenticatedIdentity(
	t *testing.T,
) {
	authService := &fakeAuthService{}

	handler := NewIdentityHandler(authService)

	_, err := handler.RequestIdentifierLinkOTP(
		context.Background(),
		&identityv1.RequestIdentifierLinkOTPRequest{
			Identifier: &identityv1.Identifier{
				Type:  identityv1.IdentifierType_IDENTIFIER_TYPE_EMAIL,
				Value: "user@example.com",
			},
		},
	)

	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf(
			"status code is %v, expected %v",
			status.Code(err),
			codes.Unauthenticated,
		)
	}

	if authService.requestOTPCalled {
		t.Fatal(
			"auth service RequestOTP() was called without authenticated identity",
		)
	}
}

func TestIdentityHandlerVerifyIdentifierLinkOTPUsesAuthenticatedIdentity(
	t *testing.T,
) {
	authService := &fakeAuthService{}

	handler := NewIdentityHandler(authService)

	ctx := contextWithAuthenticatedPrincipal(
		context.Background(),
		authenticatedPrincipal{
			IdentityID: "identity-123",
			SessionID:  "session-456",
		},
	)

	response, err := handler.VerifyIdentifierLinkOTP(
		ctx,
		&identityv1.VerifyIdentifierLinkOTPRequest{
			ChallengeId: "  otp_ch_link_test  ",
			Code:        "  123456  ",
		},
	)
	if err != nil {
		t.Fatalf(
			"VerifyIdentifierLinkOTP() returned an error: %v",
			err,
		)
	}

	if !authService.verifyOTPCalled {
		t.Fatal(
			"auth service VerifyOTP() was not called",
		)
	}

	if authService.verifyOTPInput.ChallengeID !=
		"otp_ch_link_test" {
		t.Fatalf(
			"challenge ID = %q, expected %q",
			authService.verifyOTPInput.ChallengeID,
			"otp_ch_link_test",
		)
	}

	if authService.verifyOTPInput.Code != "123456" {
		t.Fatalf(
			"OTP code = %q, expected %q",
			authService.verifyOTPInput.Code,
			"123456",
		)
	}

	if authService.verifyOTPInput.ExpectedPurpose !=
		auth.OTPPurposeLinkIdentifier {
		t.Fatalf(
			"expected purpose = %q, expected %q",
			authService.verifyOTPInput.ExpectedPurpose,
			auth.OTPPurposeLinkIdentifier,
		)
	}

	if authService.verifyOTPInput.ExpectedTargetIdentityID == nil {
		t.Fatal(
			"expected target identity ID is nil",
		)
	}

	if *authService.verifyOTPInput.ExpectedTargetIdentityID !=
		"identity-123" {
		t.Fatalf(
			"expected target identity ID = %q, expected %q",
			*authService.verifyOTPInput.ExpectedTargetIdentityID,
			"identity-123",
		)
	}

	if response == nil {
		t.Fatal(
			"VerifyIdentifierLinkOTP() returned nil response",
		)
	}
}

func TestIdentityHandlerVerifyIdentifierLinkOTPRejectsMissingAuthenticatedIdentity(
	t *testing.T,
) {
	authService := &fakeAuthService{}

	handler := NewIdentityHandler(authService)

	_, err := handler.VerifyIdentifierLinkOTP(
		context.Background(),
		&identityv1.VerifyIdentifierLinkOTPRequest{
			ChallengeId: "otp_ch_link_test",
			Code:        "123456",
		},
	)

	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf(
			"status code is %v, expected %v",
			status.Code(err),
			codes.Unauthenticated,
		)
	}

	if authService.verifyOTPCalled {
		t.Fatal(
			"auth service VerifyOTP() was called without authenticated identity",
		)
	}
}

func TestIdentityHandlerVerifyIdentifierLinkOTPMapsTargetMismatchToFailedPrecondition(
	t *testing.T,
) {
	authService := &fakeAuthService{
		verifyOTPErr: auth.ErrOTPChallengeTargetMismatch,
	}

	handler := NewIdentityHandler(authService)

	ctx := contextWithAuthenticatedPrincipal(
		context.Background(),
		authenticatedPrincipal{
			IdentityID: "identity-123",
			SessionID:  "session-456",
		},
	)

	_, err := handler.VerifyIdentifierLinkOTP(
		ctx,
		&identityv1.VerifyIdentifierLinkOTPRequest{
			ChallengeId: "otp_ch_link_test",
			Code:        "123456",
		},
	)

	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf(
			"status code is %v, expected %v",
			status.Code(err),
			codes.FailedPrecondition,
		)
	}

	if status.Convert(err).Message() !=
		"OTP challenge is not valid for identifier linking" {
		t.Fatalf(
			"error message is %q, expected %q",
			status.Convert(err).Message(),
			"OTP challenge is not valid for identifier linking",
		)
	}
}

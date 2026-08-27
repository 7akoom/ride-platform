package grpc

import (
	"context"
	"errors"
	"testing"

	identityv1 "github.com/7akoom/ride-platform/gen/go/ride/identity/v1"
	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestIdentityHandlerRequestIdentifierUnlinkOTPUsesAuthenticatedIdentity(
	t *testing.T,
) {
	authService := &fakeAuthService{
		requestIdentifierUnlinkOTPResult: auth.RequestIdentifierUnlinkOTPResult{
			ChallengeID:      "otp_ch_unlink_test",
			ExpiresInSeconds: 300,
		},
	}

	handler := NewIdentityHandler(authService)

	ctx := metadata.NewIncomingContext(
		context.Background(),
		metadata.Pairs(
			"accept-language",
			"en-US",
		),
	)

	ctx = contextWithAuthenticatedPrincipal(
		ctx,
		authenticatedPrincipal{
			IdentityID: "identity-123",
			SessionID:  "session-456",
		},
	)

	response, err := handler.RequestIdentifierUnlinkOTP(
		ctx,
		&identityv1.RequestIdentifierUnlinkOTPRequest{
			Identifier: &identityv1.Identifier{
				Type:  identityv1.IdentifierType_IDENTIFIER_TYPE_EMAIL,
				Value: "  user@example.com  ",
			},
		},
	)
	if err != nil {
		t.Fatalf(
			"RequestIdentifierUnlinkOTP() returned an error: %v",
			err,
		)
	}

	if !authService.requestIdentifierUnlinkOTPCalled {
		t.Fatal(
			"auth service RequestIdentifierUnlinkOTP() was not called",
		)
	}

	if authService.requestIdentifierUnlinkOTPInput.IdentityID !=
		"identity-123" {
		t.Fatalf(
			"identity ID = %q, expected %q",
			authService.requestIdentifierUnlinkOTPInput.IdentityID,
			"identity-123",
		)
	}

	expectedIdentifier := auth.Identifier{
		Type:  auth.IdentifierTypeEmail,
		Value: "user@example.com",
	}

	if authService.requestIdentifierUnlinkOTPInput.TargetIdentifier !=
		expectedIdentifier {
		t.Fatalf(
			"target identifier = %+v, expected %+v",
			authService.requestIdentifierUnlinkOTPInput.TargetIdentifier,
			expectedIdentifier,
		)
	}

	if authService.requestIdentifierUnlinkOTPInput.Locale != "en" {
		t.Fatalf(
			"auth service received locale %q, expected %q",
			authService.requestIdentifierUnlinkOTPInput.Locale,
			"en",
		)
	}

	if response.GetChallengeId() != "otp_ch_unlink_test" {
		t.Fatalf(
			"ChallengeId = %q, expected %q",
			response.GetChallengeId(),
			"otp_ch_unlink_test",
		)
	}

	if response.GetExpiresInSeconds() != 300 {
		t.Fatalf(
			"ExpiresInSeconds = %d, expected 300",
			response.GetExpiresInSeconds(),
		)
	}
}

func TestIdentityHandlerRequestIdentifierUnlinkOTPRejectsMissingAuthenticatedIdentity(
	t *testing.T,
) {
	authService := &fakeAuthService{}

	handler := NewIdentityHandler(authService)

	_, err := handler.RequestIdentifierUnlinkOTP(
		context.Background(),
		&identityv1.RequestIdentifierUnlinkOTPRequest{
			Identifier: &identityv1.Identifier{
				Type:  identityv1.IdentifierType_IDENTIFIER_TYPE_EMAIL,
				Value: "user@example.com",
			},
		},
	)

	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf(
			"status code = %v, expected %v",
			status.Code(err),
			codes.Unauthenticated,
		)
	}

	if authService.requestIdentifierUnlinkOTPCalled {
		t.Fatal(
			"auth service RequestIdentifierUnlinkOTP() was called without authenticated identity",
		)
	}
}

func TestIdentityHandlerVerifyIdentifierUnlinkOTPUsesAuthenticatedIdentity(
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

	response, err := handler.VerifyIdentifierUnlinkOTP(
		ctx,
		&identityv1.VerifyIdentifierUnlinkOTPRequest{
			ChallengeId: "  otp_ch_unlink_test  ",
			Code:        "  123456  ",
		},
	)
	if err != nil {
		t.Fatalf(
			"VerifyIdentifierUnlinkOTP() returned an error: %v",
			err,
		)
	}

	if !authService.verifyOTPCalled {
		t.Fatal(
			"auth service VerifyOTP() was not called",
		)
	}

	if authService.verifyOTPInput.ChallengeID !=
		"otp_ch_unlink_test" {
		t.Fatalf(
			"challenge ID = %q, expected %q",
			authService.verifyOTPInput.ChallengeID,
			"otp_ch_unlink_test",
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
		auth.OTPPurposeUnlinkIdentifier {
		t.Fatalf(
			"expected purpose = %q, expected %q",
			authService.verifyOTPInput.ExpectedPurpose,
			auth.OTPPurposeUnlinkIdentifier,
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
			"VerifyIdentifierUnlinkOTP() returned nil response",
		)
	}
}

func TestIdentityHandlerVerifyIdentifierUnlinkOTPRejectsMissingAuthenticatedIdentity(
	t *testing.T,
) {
	authService := &fakeAuthService{}

	handler := NewIdentityHandler(authService)

	_, err := handler.VerifyIdentifierUnlinkOTP(
		context.Background(),
		&identityv1.VerifyIdentifierUnlinkOTPRequest{
			ChallengeId: "otp_ch_unlink_test",
			Code:        "123456",
		},
	)

	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf(
			"status code = %v, expected %v",
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
func TestIdentityHandlerRequestIdentifierUnlinkOTPMapsErrors(
	t *testing.T,
) {
	tests := []struct {
		name         string
		serviceErr   error
		expectedCode codes.Code
	}{
		{
			name:         "invalid phone number",
			serviceErr:   auth.ErrInvalidPhoneNumber,
			expectedCode: codes.InvalidArgument,
		},
		{
			name:         "invalid email address",
			serviceErr:   auth.ErrInvalidEmailAddress,
			expectedCode: codes.InvalidArgument,
		},
		{
			name:         "identity not found",
			serviceErr:   auth.ErrIdentityNotFound,
			expectedCode: codes.NotFound,
		},
		{
			name:         "identifier not linked",
			serviceErr:   auth.ErrIdentifierNotLinked,
			expectedCode: codes.FailedPrecondition,
		},
		{
			name:         "last identifier removal",
			serviceErr:   auth.ErrLastIdentifierRemoval,
			expectedCode: codes.FailedPrecondition,
		},
		{
			name:         "rate limited",
			serviceErr:   auth.ErrOTPRequestRateLimited,
			expectedCode: codes.ResourceExhausted,
		},
		{
			name:         "unexpected error",
			serviceErr:   errors.New("unexpected unlink request failure"),
			expectedCode: codes.Internal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authService := &fakeAuthService{
				requestIdentifierUnlinkOTPErr: tt.serviceErr,
			}

			handler := NewIdentityHandler(authService)

			ctx := contextWithAuthenticatedPrincipal(
				context.Background(),
				authenticatedPrincipal{
					IdentityID: "identity-123",
					SessionID:  "session-456",
				},
			)

			_, err := handler.RequestIdentifierUnlinkOTP(
				ctx,
				&identityv1.RequestIdentifierUnlinkOTPRequest{
					Identifier: &identityv1.Identifier{
						Type:  identityv1.IdentifierType_IDENTIFIER_TYPE_EMAIL,
						Value: "user@example.com",
					},
				},
			)

			if status.Code(err) != tt.expectedCode {
				t.Fatalf(
					"status code = %v, expected %v",
					status.Code(err),
					tt.expectedCode,
				)
			}
		})
	}
}

func TestIdentityHandlerVerifyIdentifierUnlinkOTPMapsErrors(
	t *testing.T,
) {
	tests := []struct {
		name         string
		serviceErr   error
		expectedCode codes.Code
	}{
		{
			name:         "challenge not found",
			serviceErr:   auth.ErrChallengeNotFound,
			expectedCode: codes.NotFound,
		},
		{
			name:         "challenge expired",
			serviceErr:   auth.ErrChallengeExpired,
			expectedCode: codes.FailedPrecondition,
		},
		{
			name:         "challenge used",
			serviceErr:   auth.ErrChallengeUsed,
			expectedCode: codes.FailedPrecondition,
		},
		{
			name:         "challenge cancelled",
			serviceErr:   auth.ErrChallengeCancelled,
			expectedCode: codes.FailedPrecondition,
		},
		{
			name:         "attempts exceeded",
			serviceErr:   auth.ErrChallengeAttemptsExceeded,
			expectedCode: codes.ResourceExhausted,
		},
		{
			name:         "purpose mismatch",
			serviceErr:   auth.ErrOTPPurposeMismatch,
			expectedCode: codes.FailedPrecondition,
		},
		{
			name:         "target identity mismatch",
			serviceErr:   auth.ErrOTPChallengeTargetMismatch,
			expectedCode: codes.FailedPrecondition,
		},
		{
			name:         "invalid OTP",
			serviceErr:   auth.ErrInvalidOTP,
			expectedCode: codes.Unauthenticated,
		},
		{
			name:         "identity not found",
			serviceErr:   auth.ErrIdentityNotFound,
			expectedCode: codes.NotFound,
		},
		{
			name:         "identifier not linked",
			serviceErr:   auth.ErrIdentifierNotLinked,
			expectedCode: codes.FailedPrecondition,
		},
		{
			name:         "last identifier removal",
			serviceErr:   auth.ErrLastIdentifierRemoval,
			expectedCode: codes.FailedPrecondition,
		},
		{
			name:         "unexpected error",
			serviceErr:   errors.New("unexpected unlink verification failure"),
			expectedCode: codes.Internal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authService := &fakeAuthService{
				verifyOTPErr: tt.serviceErr,
			}

			handler := NewIdentityHandler(authService)

			ctx := contextWithAuthenticatedPrincipal(
				context.Background(),
				authenticatedPrincipal{
					IdentityID: "identity-123",
					SessionID:  "session-456",
				},
			)

			_, err := handler.VerifyIdentifierUnlinkOTP(
				ctx,
				&identityv1.VerifyIdentifierUnlinkOTPRequest{
					ChallengeId: "otp_ch_unlink_test",
					Code:        "123456",
				},
			)

			if status.Code(err) != tt.expectedCode {
				t.Fatalf(
					"status code = %v, expected %v",
					status.Code(err),
					tt.expectedCode,
				)
			}
		})
	}
}

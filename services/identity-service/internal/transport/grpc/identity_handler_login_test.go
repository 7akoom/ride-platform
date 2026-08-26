package grpc

import (
	"context"
	"errors"
	"net"
	"testing"

	identityv1 "github.com/7akoom/ride-platform/gen/go/ride/identity/v1"
	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

func TestIdentityHandlerRequestLoginOTPReturnsChallenge(t *testing.T) {
	authService := &fakeAuthService{
		requestOTPResult: auth.RequestOTPResult{
			ChallengeID:      "otp_ch_test",
			ExpiresInSeconds: 300,
		},
	}

	handler := NewIdentityHandler(authService)

	response, err := handler.RequestLoginOTP(
		context.Background(),
		&identityv1.RequestLoginOTPRequest{
			Identifier: &identityv1.Identifier{
				Type:  identityv1.IdentifierType_IDENTIFIER_TYPE_PHONE,
				Value: "  +9647500000000  ",
			},
		},
	)
	if err != nil {
		t.Fatalf("RequestLoginOTP() returned an error: %v", err)
	}

	if !authService.requestOTPCalled {
		t.Fatal("auth service RequestOTP() was not called")
	}

	expectedIdentifier := auth.Identifier{
		Type:  auth.IdentifierTypePhone,
		Value: "+9647500000000",
	}

	if authService.requestOTPInput.Identifier != expectedIdentifier {
		t.Fatalf(
			"auth service received identifier %+v, expected %+v",
			authService.requestOTPInput.Identifier,
			expectedIdentifier,
		)
	}

	if authService.requestOTPInput.Purpose != auth.OTPPurposeLogin {
		t.Fatalf(
			"auth service received purpose %q, expected %q",
			authService.requestOTPInput.Purpose,
			auth.OTPPurposeLogin,
		)
	}

	if response.GetChallengeId() != "otp_ch_test" {
		t.Fatalf(
			"ChallengeId is %q, expected %q",
			response.GetChallengeId(),
			"otp_ch_test",
		)
	}

	if response.GetExpiresInSeconds() != 300 {
		t.Fatalf(
			"ExpiresInSeconds is %d, expected 300",
			response.GetExpiresInSeconds(),
		)
	}
}

func TestIdentityHandlerRequestLoginOTPRejectsNilRequest(t *testing.T) {
	authService := &fakeAuthService{}

	handler := NewIdentityHandler(authService)

	_, err := handler.RequestLoginOTP(
		context.Background(),
		nil,
	)

	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf(
			"status code is %v, expected %v",
			status.Code(err),
			codes.InvalidArgument,
		)
	}

	if authService.requestOTPCalled {
		t.Fatal(
			"auth service RequestOTP() was called for nil request",
		)
	}
}

func TestIdentityHandlerRequestLoginOTPRejectsEmptyIdentifierValue(
	t *testing.T,
) {
	authService := &fakeAuthService{}

	handler := NewIdentityHandler(authService)

	_, err := handler.RequestLoginOTP(
		context.Background(),
		&identityv1.RequestLoginOTPRequest{
			Identifier: &identityv1.Identifier{
				Type:  identityv1.IdentifierType_IDENTIFIER_TYPE_PHONE,
				Value: "   ",
			},
		},
	)

	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf(
			"status code is %v, expected %v",
			status.Code(err),
			codes.InvalidArgument,
		)
	}

	if authService.requestOTPCalled {
		t.Fatal(
			"auth service RequestOTP() was called for empty identifier value",
		)
	}
}

func TestIdentityHandlerRequestLoginOTPMapsInvalidPhoneNumberToInvalidArgument(
	t *testing.T,
) {
	authService := &fakeAuthService{
		requestOTPErr: auth.ErrInvalidPhoneNumber,
	}

	handler := NewIdentityHandler(authService)

	_, err := handler.RequestLoginOTP(
		context.Background(),
		&identityv1.RequestLoginOTPRequest{
			Identifier: &identityv1.Identifier{
				Type:  identityv1.IdentifierType_IDENTIFIER_TYPE_PHONE,
				Value: "07501234567",
			},
		},
	)

	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf(
			"status code is %v, expected %v",
			status.Code(err),
			codes.InvalidArgument,
		)
	}

	if status.Convert(err).Message() != "invalid phone number" {
		t.Fatalf(
			"error message is %q, expected %q",
			status.Convert(err).Message(),
			"invalid phone number",
		)
	}
}

func TestIdentityHandlerRequestLoginOTPMapsServiceFailureToInternal(
	t *testing.T,
) {
	authService := &fakeAuthService{
		requestOTPErr: errors.New("database unavailable"),
	}

	handler := NewIdentityHandler(authService)

	_, err := handler.RequestLoginOTP(
		context.Background(),
		&identityv1.RequestLoginOTPRequest{
			Identifier: &identityv1.Identifier{
				Type:  identityv1.IdentifierType_IDENTIFIER_TYPE_PHONE,
				Value: "+9647500000000",
			},
		},
	)

	if status.Code(err) != codes.Internal {
		t.Fatalf(
			"status code is %v, expected %v",
			status.Code(err),
			codes.Internal,
		)
	}

	if status.Convert(err).Message() != "failed to request login OTP" {
		t.Fatalf(
			"error message is %q, expected %q",
			status.Convert(err).Message(),
			"failed to request login OTP",
		)
	}
}

func TestIdentityHandlerVerifyLoginOTPReturnsTokens(t *testing.T) {
	authService := &fakeAuthService{
		verifyOTPResult: auth.VerifyOTPResult{
			IdentityID:                  "identity-test-id",
			AccessToken:                 "access-token",
			RefreshToken:                "refresh-token",
			AccessTokenExpiresInSeconds: 900,
		},
	}

	handler := NewIdentityHandler(authService)

	ctx := metadata.NewIncomingContext(
		context.Background(),
		metadata.Pairs(
			"user-agent",
			" ride-app/1.0.0 ",
		),
	)

	ctx = peer.NewContext(
		ctx,
		&peer.Peer{
			Addr: &net.TCPAddr{
				IP:   net.ParseIP("192.0.2.10"),
				Port: 443,
			},
		},
	)

	response, err := handler.VerifyLoginOTP(
		ctx,
		&identityv1.VerifyLoginOTPRequest{
			ChallengeId: "  otp_ch_test  ",
			Code:        "  123456  ",
			ClientId:    " mobile-app ",
			DeviceId:    " device-123 ",
			DeviceName:  " iPhone 15 Pro ",
			Platform:    " ios ",
			AppVersion:  " 1.0.0 ",
		},
	)
	if err != nil {
		t.Fatalf("VerifyLoginOTP() returned an error: %v", err)
	}

	if !authService.verifyOTPCalled {
		t.Fatal("auth service VerifyOTP() was not called")
	}

	if authService.verifyOTPInput.ChallengeID != "otp_ch_test" {
		t.Fatalf(
			"auth service received challenge ID %q, expected %q",
			authService.verifyOTPInput.ChallengeID,
			"otp_ch_test",
		)
	}

	if authService.verifyOTPInput.Code != "123456" {
		t.Fatalf(
			"auth service received OTP code %q, expected %q",
			authService.verifyOTPInput.Code,
			"123456",
		)
	}

	if authService.verifyOTPInput.ExpectedPurpose != auth.OTPPurposeLogin {
		t.Fatalf(
			"auth service received purpose %q, expected %q",
			authService.verifyOTPInput.ExpectedPurpose,
			auth.OTPPurposeLogin,
		)
	}

	expectedSessionMetadata := auth.SessionMetadata{
		ClientID:   "mobile-app",
		DeviceID:   "device-123",
		DeviceName: "iPhone 15 Pro",
		Platform:   "ios",
		AppVersion: "1.0.0",
		IPAddress:  "192.0.2.10",
		UserAgent:  "ride-app/1.0.0",
	}

	if authService.verifyOTPInput.SessionMetadata !=
		expectedSessionMetadata {
		t.Fatalf(
			"auth service received session metadata %+v, expected %+v",
			authService.verifyOTPInput.SessionMetadata,
			expectedSessionMetadata,
		)
	}

	if response.GetIdentityId() != "identity-test-id" {
		t.Fatalf(
			"IdentityId is %q, expected %q",
			response.GetIdentityId(),
			"identity-test-id",
		)
	}

	if response.GetAccessToken() != "access-token" {
		t.Fatal("unexpected access token")
	}

	if response.GetRefreshToken() != "refresh-token" {
		t.Fatal("unexpected refresh token")
	}

	if response.GetAccessTokenExpiresInSeconds() != 900 {
		t.Fatalf(
			"AccessTokenExpiresInSeconds is %d, expected 900",
			response.GetAccessTokenExpiresInSeconds(),
		)
	}
}

func TestIdentityHandlerVerifyLoginOTPRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name    string
		request *identityv1.VerifyLoginOTPRequest
	}{
		{
			name:    "nil request",
			request: nil,
		},
		{
			name: "empty challenge ID",
			request: &identityv1.VerifyLoginOTPRequest{
				ChallengeId: "   ",
				Code:        "123456",
			},
		},
		{
			name: "empty OTP code",
			request: &identityv1.VerifyLoginOTPRequest{
				ChallengeId: "otp_ch_test",
				Code:        "   ",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			authService := &fakeAuthService{}
			handler := NewIdentityHandler(authService)

			_, err := handler.VerifyLoginOTP(
				context.Background(),
				test.request,
			)

			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf(
					"status code is %v, expected %v",
					status.Code(err),
					codes.InvalidArgument,
				)
			}

			if authService.verifyOTPCalled {
				t.Fatal(
					"auth service VerifyOTP() was called for invalid input",
				)
			}
		})
	}
}

func TestIdentityHandlerRequestLoginOTPMapsRateLimitToResourceExhausted(
	t *testing.T,
) {
	authService := &fakeAuthService{
		requestOTPErr: auth.ErrOTPRequestRateLimited,
	}

	handler := NewIdentityHandler(authService)

	_, err := handler.RequestLoginOTP(
		context.Background(),
		&identityv1.RequestLoginOTPRequest{
			Identifier: &identityv1.Identifier{
				Type:  identityv1.IdentifierType_IDENTIFIER_TYPE_PHONE,
				Value: "+9647501234567",
			},
		},
	)

	if status.Code(err) != codes.ResourceExhausted {
		t.Fatalf(
			"status code is %v, expected %v",
			status.Code(err),
			codes.ResourceExhausted,
		)
	}

	if status.Convert(err).Message() !=
		"OTP request rate limit exceeded" {
		t.Fatalf(
			"error message is %q, expected %q",
			status.Convert(err).Message(),
			"OTP request rate limit exceeded",
		)
	}
}

func TestIdentityHandlerVerifyLoginOTPMapsApplicationErrors(t *testing.T) {
	tests := []struct {
		name         string
		serviceError error
		expectedCode codes.Code
	}{
		{
			name:         "challenge not found",
			serviceError: auth.ErrChallengeNotFound,
			expectedCode: codes.NotFound,
		},
		{
			name:         "challenge expired",
			serviceError: auth.ErrChallengeExpired,
			expectedCode: codes.FailedPrecondition,
		},
		{
			name:         "challenge already used",
			serviceError: auth.ErrChallengeUsed,
			expectedCode: codes.FailedPrecondition,
		},
		{
			name:         "challenge cancelled",
			serviceError: auth.ErrChallengeCancelled,
			expectedCode: codes.FailedPrecondition,
		},
		{
			name:         "challenge attempts exceeded",
			serviceError: auth.ErrChallengeAttemptsExceeded,
			expectedCode: codes.ResourceExhausted,
		},
		{
			name:         "OTP purpose mismatch",
			serviceError: auth.ErrOTPPurposeMismatch,
			expectedCode: codes.FailedPrecondition,
		},
		{
			name:         "invalid OTP",
			serviceError: auth.ErrInvalidOTP,
			expectedCode: codes.Unauthenticated,
		},
		{
			name:         "identity inactive",
			serviceError: auth.ErrIdentityInactive,
			expectedCode: codes.PermissionDenied,
		},
		{
			name:         "unexpected internal failure",
			serviceError: errors.New("database unavailable"),
			expectedCode: codes.Internal,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			authService := &fakeAuthService{
				verifyOTPErr: test.serviceError,
			}

			handler := NewIdentityHandler(authService)

			_, err := handler.VerifyLoginOTP(
				context.Background(),
				&identityv1.VerifyLoginOTPRequest{
					ChallengeId: "otp_ch_test",
					Code:        "123456",
				},
			)

			if status.Code(err) != test.expectedCode {
				t.Fatalf(
					"status code is %v, expected %v",
					status.Code(err),
					test.expectedCode,
				)
			}
		})
	}
}

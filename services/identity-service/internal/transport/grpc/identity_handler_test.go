package grpc

import (
	"context"
	"errors"
	"testing"

	identityv1 "github.com/7akoom/ride-platform/gen/go/ride/identity/v1"
	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeAuthService struct {
	requestOTPResult auth.RequestOTPResult
	requestOTPErr    error
	requestOTPInput  auth.RequestOTPInput
	requestOTPCalled bool

	verifyOTPResult auth.VerifyOTPResult
	verifyOTPErr    error
	verifyOTPInput  auth.VerifyOTPInput
	verifyOTPCalled bool

	refreshTokenResult auth.RefreshTokenResult
	refreshTokenErr    error
	refreshTokenInput  auth.RefreshTokenInput
	refreshTokenCalled bool
}

func (f *fakeAuthService) RequestOTP(
	ctx context.Context,
	input auth.RequestOTPInput,
) (auth.RequestOTPResult, error) {
	f.requestOTPCalled = true
	f.requestOTPInput = input

	if f.requestOTPErr != nil {
		return auth.RequestOTPResult{}, f.requestOTPErr
	}

	return f.requestOTPResult, nil
}

func (f *fakeAuthService) VerifyOTP(
	ctx context.Context,
	input auth.VerifyOTPInput,
) (auth.VerifyOTPResult, error) {
	f.verifyOTPCalled = true
	f.verifyOTPInput = input

	if f.verifyOTPErr != nil {
		return auth.VerifyOTPResult{}, f.verifyOTPErr
	}

	return f.verifyOTPResult, nil
}

func (f *fakeAuthService) RefreshToken(
	ctx context.Context,
	input auth.RefreshTokenInput,
) (auth.RefreshTokenResult, error) {
	f.refreshTokenCalled = true
	f.refreshTokenInput = input

	if f.refreshTokenErr != nil {
		return auth.RefreshTokenResult{}, f.refreshTokenErr
	}

	return f.refreshTokenResult, nil
}

func TestIdentityHandlerRequestOTPReturnsChallenge(t *testing.T) {
	authService := &fakeAuthService{
		requestOTPResult: auth.RequestOTPResult{
			ChallengeID:      "otp_ch_test",
			ExpiresInSeconds: 300,
		},
	}

	handler := NewIdentityHandler(authService)

	response, err := handler.RequestOTP(
		context.Background(),
		&identityv1.RequestOTPRequest{
			PhoneNumber: "  +9647500000000  ",
		},
	)
	if err != nil {
		t.Fatalf("RequestOTP() returned an error: %v", err)
	}

	if !authService.requestOTPCalled {
		t.Fatal("auth service RequestOTP() was not called")
	}

	if authService.requestOTPInput.PhoneNumber != "+9647500000000" {
		t.Fatalf(
			"auth service received phone number %q, expected %q",
			authService.requestOTPInput.PhoneNumber,
			"+9647500000000",
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

func TestIdentityHandlerRequestOTPRejectsNilRequest(t *testing.T) {
	authService := &fakeAuthService{}

	handler := NewIdentityHandler(authService)

	_, err := handler.RequestOTP(
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

func TestIdentityHandlerRequestOTPRejectsEmptyPhoneNumber(t *testing.T) {
	authService := &fakeAuthService{}

	handler := NewIdentityHandler(authService)

	_, err := handler.RequestOTP(
		context.Background(),
		&identityv1.RequestOTPRequest{
			PhoneNumber: "   ",
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
			"auth service RequestOTP() was called for empty phone number",
		)
	}
}

func TestIdentityHandlerRequestOTPMapsInvalidPhoneNumberToInvalidArgument(
	t *testing.T,
) {
	authService := &fakeAuthService{
		requestOTPErr: auth.ErrInvalidPhoneNumber,
	}

	handler := NewIdentityHandler(authService)

	_, err := handler.RequestOTP(
		context.Background(),
		&identityv1.RequestOTPRequest{
			PhoneNumber: "07501234567",
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

func TestIdentityHandlerRequestOTPMapsServiceFailureToInternal(
	t *testing.T,
) {
	authService := &fakeAuthService{
		requestOTPErr: errors.New("database unavailable"),
	}

	handler := NewIdentityHandler(authService)

	_, err := handler.RequestOTP(
		context.Background(),
		&identityv1.RequestOTPRequest{
			PhoneNumber: "+9647500000000",
		},
	)

	if status.Code(err) != codes.Internal {
		t.Fatalf(
			"status code is %v, expected %v",
			status.Code(err),
			codes.Internal,
		)
	}

	if status.Convert(err).Message() != "failed to request OTP" {
		t.Fatalf(
			"error message is %q, expected %q",
			status.Convert(err).Message(),
			"failed to request OTP",
		)
	}
}

func TestIdentityHandlerVerifyOTPReturnsTokens(t *testing.T) {
	authService := &fakeAuthService{
		verifyOTPResult: auth.VerifyOTPResult{
			IdentityID:                  "identity-test-id",
			AccessToken:                 "access-token",
			RefreshToken:                "refresh-token",
			AccessTokenExpiresInSeconds: 900,
		},
	}

	handler := NewIdentityHandler(authService)

	response, err := handler.VerifyOTP(
		context.Background(),
		&identityv1.VerifyOTPRequest{
			ChallengeId: "  otp_ch_test  ",
			Code:        "  123456  ",
		},
	)
	if err != nil {
		t.Fatalf("VerifyOTP() returned an error: %v", err)
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

func TestIdentityHandlerVerifyOTPRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name    string
		request *identityv1.VerifyOTPRequest
	}{
		{
			name:    "nil request",
			request: nil,
		},
		{
			name: "empty challenge ID",
			request: &identityv1.VerifyOTPRequest{
				ChallengeId: "   ",
				Code:        "123456",
			},
		},
		{
			name: "empty OTP code",
			request: &identityv1.VerifyOTPRequest{
				ChallengeId: "otp_ch_test",
				Code:        "   ",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			authService := &fakeAuthService{}
			handler := NewIdentityHandler(authService)

			_, err := handler.VerifyOTP(
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

func TestIdentityHandlerRequestOTPMapsRateLimitToResourceExhausted(
	t *testing.T,
) {
	authService := &fakeAuthService{
		requestOTPErr: auth.ErrOTPRequestRateLimited,
	}

	handler := NewIdentityHandler(authService)

	_, err := handler.RequestOTP(
		context.Background(),
		&identityv1.RequestOTPRequest{
			PhoneNumber: "+9647501234567",
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

func TestIdentityHandlerVerifyOTPMapsApplicationErrors(t *testing.T) {
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

			_, err := handler.VerifyOTP(
				context.Background(),
				&identityv1.VerifyOTPRequest{
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

func TestIdentityHandlerRefreshTokenReturnsRotatedTokens(
	t *testing.T,
) {
	authService := &fakeAuthService{
		refreshTokenResult: auth.RefreshTokenResult{
			IdentityID:                  "identity-123",
			AccessToken:                 "new-access-token",
			RefreshToken:                "new-refresh-token",
			AccessTokenExpiresInSeconds: 900,
		},
	}

	handler := NewIdentityHandler(
		authService,
	)

	response, err := handler.RefreshToken(
		context.Background(),
		&identityv1.RefreshTokenRequest{
			RefreshToken: "current-refresh-token",
		},
	)
	if err != nil {
		t.Fatalf(
			"RefreshToken() returned an error: %v",
			err,
		)
	}

	if !authService.refreshTokenCalled {
		t.Fatal(
			"auth service RefreshToken() was not called",
		)
	}

	if authService.refreshTokenInput.RefreshToken !=
		"current-refresh-token" {
		t.Fatalf(
			"auth service received refresh token %q, expected %q",
			authService.refreshTokenInput.RefreshToken,
			"current-refresh-token",
		)
	}

	if response.GetIdentityId() != "identity-123" {
		t.Fatalf(
			"IdentityId = %q, expected %q",
			response.GetIdentityId(),
			"identity-123",
		)
	}

	if response.GetAccessToken() != "new-access-token" {
		t.Fatalf(
			"AccessToken = %q, expected %q",
			response.GetAccessToken(),
			"new-access-token",
		)
	}

	if response.GetRefreshToken() != "new-refresh-token" {
		t.Fatalf(
			"RefreshToken = %q, expected %q",
			response.GetRefreshToken(),
			"new-refresh-token",
		)
	}

	if response.GetAccessTokenExpiresInSeconds() != 900 {
		t.Fatalf(
			"AccessTokenExpiresInSeconds = %d, expected 900",
			response.GetAccessTokenExpiresInSeconds(),
		)
	}
}

func TestIdentityHandlerRefreshTokenRejectsInvalidInput(
	t *testing.T,
) {
	tests := []struct {
		name    string
		request *identityv1.RefreshTokenRequest
	}{
		{
			name:    "nil request",
			request: nil,
		},
		{
			name: "empty refresh token",
			request: &identityv1.RefreshTokenRequest{
				RefreshToken: "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(
			tt.name,
			func(t *testing.T) {
				authService := &fakeAuthService{}

				handler := NewIdentityHandler(
					authService,
				)

				_, err := handler.RefreshToken(
					context.Background(),
					tt.request,
				)

				if status.Code(err) != codes.InvalidArgument {
					t.Fatalf(
						"RefreshToken() code = %v, expected %v",
						status.Code(err),
						codes.InvalidArgument,
					)
				}

				if authService.refreshTokenCalled {
					t.Fatal(
						"auth service RefreshToken() was called for invalid input",
					)
				}
			},
		)
	}
}

func TestIdentityHandlerRefreshTokenMapsApplicationErrors(
	t *testing.T,
) {
	tests := []struct {
		name         string
		serviceErr   error
		expectedCode codes.Code
	}{
		{
			name:         "invalid refresh token",
			serviceErr:   auth.ErrInvalidRefreshToken,
			expectedCode: codes.Unauthenticated,
		},
		{
			name:         "refresh token expired",
			serviceErr:   auth.ErrRefreshTokenExpired,
			expectedCode: codes.Unauthenticated,
		},
		{
			name:         "refresh token revoked",
			serviceErr:   auth.ErrRefreshTokenRevoked,
			expectedCode: codes.Unauthenticated,
		},
		{
			name:         "refresh token reused",
			serviceErr:   auth.ErrRefreshTokenReused,
			expectedCode: codes.Unauthenticated,
		},
		{
			name:         "session expired",
			serviceErr:   auth.ErrSessionExpired,
			expectedCode: codes.Unauthenticated,
		},
		{
			name:         "session revoked",
			serviceErr:   auth.ErrSessionRevoked,
			expectedCode: codes.Unauthenticated,
		},
		{
			name:         "identity inactive",
			serviceErr:   auth.ErrIdentityInactive,
			expectedCode: codes.PermissionDenied,
		},
		{
			name:         "unexpected internal failure",
			serviceErr:   errors.New("unexpected failure"),
			expectedCode: codes.Internal,
		},
	}

	for _, tt := range tests {
		t.Run(
			tt.name,
			func(t *testing.T) {
				authService := &fakeAuthService{
					refreshTokenErr: tt.serviceErr,
				}

				handler := NewIdentityHandler(
					authService,
				)

				_, err := handler.RefreshToken(
					context.Background(),
					&identityv1.RefreshTokenRequest{
						RefreshToken: "current-refresh-token",
					},
				)

				if status.Code(err) != tt.expectedCode {
					t.Fatalf(
						"RefreshToken() code = %v, expected %v",
						status.Code(err),
						tt.expectedCode,
					)
				}

				if !authService.refreshTokenCalled {
					t.Fatal(
						"auth service RefreshToken() was not called",
					)
				}
			},
		)
	}
}
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
		{
			name: "spaces only refresh token",
			request: &identityv1.RefreshTokenRequest{
				RefreshToken: "   ",
			},
		},
		{
			name: "tabs and newlines refresh token",
			request: &identityv1.RefreshTokenRequest{
				RefreshToken: "\t\n ",
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

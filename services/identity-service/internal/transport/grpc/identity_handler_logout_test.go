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

func TestIdentityHandlerLogoutRevokesCurrentSession(
	t *testing.T,
) {
	authService := &fakeAuthService{}

	handler := NewIdentityHandler(
		authService,
	)

	response, err := handler.Logout(
		context.Background(),
		&identityv1.LogoutRequest{
			RefreshToken: "current-refresh-token",
		},
	)
	if err != nil {
		t.Fatalf(
			"Logout() returned an error: %v",
			err,
		)
	}

	if response == nil {
		t.Fatal(
			"Logout() returned nil response",
		)
	}

	if !authService.logoutCalled {
		t.Fatal(
			"auth service Logout() was not called",
		)
	}

	if authService.logoutInput.RefreshToken !=
		"current-refresh-token" {
		t.Fatalf(
			"auth service received refresh token %q, expected %q",
			authService.logoutInput.RefreshToken,
			"current-refresh-token",
		)
	}
}

func TestIdentityHandlerLogoutRejectsInvalidInput(
	t *testing.T,
) {
	tests := []struct {
		name    string
		request *identityv1.LogoutRequest
	}{
		{
			name:    "nil request",
			request: nil,
		},
		{
			name: "empty refresh token",
			request: &identityv1.LogoutRequest{
				RefreshToken: "",
			},
		},
		{
			name: "spaces only refresh token",
			request: &identityv1.LogoutRequest{
				RefreshToken: "   ",
			},
		},
		{
			name: "tabs and newlines refresh token",
			request: &identityv1.LogoutRequest{
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

				_, err := handler.Logout(
					context.Background(),
					tt.request,
				)

				if status.Code(err) != codes.InvalidArgument {
					t.Fatalf(
						"Logout() code = %v, expected %v",
						status.Code(err),
						codes.InvalidArgument,
					)
				}

				if authService.logoutCalled {
					t.Fatal(
						"auth service Logout() was called for invalid input",
					)
				}
			},
		)
	}
}

func TestIdentityHandlerLogoutMapsInvalidRefreshTokenToInvalidArgument(
	t *testing.T,
) {
	authService := &fakeAuthService{
		logoutErr: auth.ErrInvalidRefreshToken,
	}

	handler := NewIdentityHandler(
		authService,
	)

	response, err := handler.Logout(
		context.Background(),
		&identityv1.LogoutRequest{
			RefreshToken: "current-refresh-token",
		},
	)

	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf(
			"Logout() code = %v, expected %v",
			status.Code(err),
			codes.InvalidArgument,
		)
	}

	if response != nil {
		t.Fatal(
			"Logout() returned a response for invalid refresh token",
		)
	}

	if !authService.logoutCalled {
		t.Fatal(
			"auth service Logout() was not called",
		)
	}
}

func TestIdentityHandlerLogoutMapsServiceFailureToInternal(
	t *testing.T,
) {
	authService := &fakeAuthService{
		logoutErr: errors.New("database failure"),
	}

	handler := NewIdentityHandler(
		authService,
	)

	response, err := handler.Logout(
		context.Background(),
		&identityv1.LogoutRequest{
			RefreshToken: "current-refresh-token",
		},
	)

	if status.Code(err) != codes.Internal {
		t.Fatalf(
			"Logout() code = %v, expected %v",
			status.Code(err),
			codes.Internal,
		)
	}

	if response != nil {
		t.Fatal(
			"Logout() returned a response on failure",
		)
	}

	if !authService.logoutCalled {
		t.Fatal(
			"auth service Logout() was not called",
		)
	}
}

func TestIdentityHandlerLogoutAllSessionsRevokesAllSessions(
	t *testing.T,
) {
	authService := &fakeAuthService{}

	handler := NewIdentityHandler(
		authService,
	)

	response, err := handler.LogoutAllSessions(
		context.Background(),
		&identityv1.LogoutAllSessionsRequest{
			RefreshToken: "current-refresh-token",
		},
	)
	if err != nil {
		t.Fatalf(
			"LogoutAllSessions() returned an error: %v",
			err,
		)
	}

	if response == nil {
		t.Fatal(
			"LogoutAllSessions() returned nil response",
		)
	}

	if !authService.logoutAllSessionsCalled {
		t.Fatal(
			"auth service LogoutAllSessions() was not called",
		)
	}

	if authService.logoutAllSessionsInput.RefreshToken !=
		"current-refresh-token" {
		t.Fatalf(
			"auth service received refresh token %q, expected %q",
			authService.logoutAllSessionsInput.RefreshToken,
			"current-refresh-token",
		)
	}
}

func TestIdentityHandlerLogoutAllSessionsRejectsInvalidInput(
	t *testing.T,
) {
	tests := []struct {
		name    string
		request *identityv1.LogoutAllSessionsRequest
	}{
		{
			name:    "nil request",
			request: nil,
		},
		{
			name: "empty refresh token",
			request: &identityv1.LogoutAllSessionsRequest{
				RefreshToken: "",
			},
		},
		{
			name: "spaces only refresh token",
			request: &identityv1.LogoutAllSessionsRequest{
				RefreshToken: "   ",
			},
		},
		{
			name: "tabs and newlines refresh token",
			request: &identityv1.LogoutAllSessionsRequest{
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

				_, err := handler.LogoutAllSessions(
					context.Background(),
					tt.request,
				)

				if status.Code(err) != codes.InvalidArgument {
					t.Fatalf(
						"LogoutAllSessions() code = %v, expected %v",
						status.Code(err),
						codes.InvalidArgument,
					)
				}

				if authService.logoutAllSessionsCalled {
					t.Fatal(
						"auth service LogoutAllSessions() was called for invalid input",
					)
				}
			},
		)
	}
}

func TestIdentityHandlerLogoutAllSessionsMapsInvalidRefreshTokenToInvalidArgument(
	t *testing.T,
) {
	authService := &fakeAuthService{
		logoutAllSessionsErr: auth.ErrInvalidRefreshToken,
	}

	handler := NewIdentityHandler(
		authService,
	)

	response, err := handler.LogoutAllSessions(
		context.Background(),
		&identityv1.LogoutAllSessionsRequest{
			RefreshToken: "current-refresh-token",
		},
	)

	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf(
			"LogoutAllSessions() code = %v, expected %v",
			status.Code(err),
			codes.InvalidArgument,
		)
	}

	if response != nil {
		t.Fatal(
			"LogoutAllSessions() returned a response for invalid refresh token",
		)
	}

	if !authService.logoutAllSessionsCalled {
		t.Fatal(
			"auth service LogoutAllSessions() was not called",
		)
	}
}
func TestIdentityHandlerLogoutAllSessionsMapsServiceFailureToInternal(
	t *testing.T,
) {
	authService := &fakeAuthService{
		logoutAllSessionsErr: errors.New("database failure"),
	}

	handler := NewIdentityHandler(
		authService,
	)

	response, err := handler.LogoutAllSessions(
		context.Background(),
		&identityv1.LogoutAllSessionsRequest{
			RefreshToken: "current-refresh-token",
		},
	)

	if status.Code(err) != codes.Internal {
		t.Fatalf(
			"LogoutAllSessions() code = %v, expected %v",
			status.Code(err),
			codes.Internal,
		)
	}

	if response != nil {
		t.Fatal(
			"LogoutAllSessions() returned a response on failure",
		)
	}

	if !authService.logoutAllSessionsCalled {
		t.Fatal(
			"auth service LogoutAllSessions() was not called",
		)
	}
}

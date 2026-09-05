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

func TestIdentityHandlerRevokeSessionRevokesOwnedSession(
	t *testing.T,
) {
	authService := &fakeAuthService{}
	handler := NewIdentityHandler(authService)

	ctx := contextWithAuthenticatedPrincipal(
		context.Background(),
		authenticatedPrincipal{
			IdentityID: "11111111-1111-1111-1111-111111111111",
			SessionID:  "22222222-2222-2222-2222-222222222222",
		},
	)

	response, err := handler.RevokeSession(
		ctx,
		&identityv1.RevokeSessionRequest{
			SessionId: "33333333-3333-3333-3333-333333333333",
		},
	)
	if err != nil {
		t.Fatalf(
			"RevokeSession() returned an error: %v",
			err,
		)
	}

	if response == nil {
		t.Fatal(
			"RevokeSession() returned nil response",
		)
	}

	if !authService.revokeSessionCalled {
		t.Fatal(
			"RevokeSession() did not call auth service",
		)
	}

	if authService.revokeSessionInput.IdentityID !=
		"11111111-1111-1111-1111-111111111111" {
		t.Fatalf(
			"auth service identity ID = %q",
			authService.revokeSessionInput.IdentityID,
		)
	}

	if authService.revokeSessionInput.SessionID !=
		"33333333-3333-3333-3333-333333333333" {
		t.Fatalf(
			"auth service session ID = %q",
			authService.revokeSessionInput.SessionID,
		)
	}
}
func TestIdentityHandlerRevokeSessionRejectsBlankSessionID(
	t *testing.T,
) {
	testCases := []struct {
		name      string
		sessionID string
	}{
		{
			name:      "empty",
			sessionID: "",
		},
		{
			name:      "spaces",
			sessionID: "   ",
		},
		{
			name:      "tabs and newlines",
			sessionID: "\t\n ",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			authService := &fakeAuthService{}
			handler := NewIdentityHandler(authService)

			ctx := contextWithAuthenticatedPrincipal(
				context.Background(),
				authenticatedPrincipal{
					IdentityID: "11111111-1111-1111-1111-111111111111",
					SessionID:  "22222222-2222-2222-2222-222222222222",
				},
			)

			response, err := handler.RevokeSession(
				ctx,
				&identityv1.RevokeSessionRequest{
					SessionId: testCase.sessionID,
				},
			)

			if response != nil {
				t.Fatal(
					"RevokeSession() returned a response, want nil",
				)
			}

			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf(
					"RevokeSession() code = %v, want %v",
					status.Code(err),
					codes.InvalidArgument,
				)
			}

			if authService.revokeSessionCalled {
				t.Fatal(
					"RevokeSession() called auth service for blank session ID",
				)
			}
		})
	}
}
func TestIdentityHandlerRevokeSessionRejectsMissingAuthenticatedIdentity(
	t *testing.T,
) {
	authService := &fakeAuthService{}
	handler := NewIdentityHandler(authService)

	response, err := handler.RevokeSession(
		context.Background(),
		&identityv1.RevokeSessionRequest{
			SessionId: "33333333-3333-3333-3333-333333333333",
		},
	)

	if response != nil {
		t.Fatal(
			"RevokeSession() returned a response, want nil",
		)
	}

	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf(
			"RevokeSession() code = %v, want %v",
			status.Code(err),
			codes.Unauthenticated,
		)
	}

	if authService.revokeSessionCalled {
		t.Fatal(
			"RevokeSession() called auth service without authenticated identity",
		)
	}
}
func TestIdentityHandlerRevokeSessionRejectsNilRequest(
	t *testing.T,
) {
	authService := &fakeAuthService{}
	handler := NewIdentityHandler(authService)

	ctx := contextWithAuthenticatedPrincipal(
		context.Background(),
		authenticatedPrincipal{
			IdentityID: "11111111-1111-1111-1111-111111111111",
			SessionID:  "22222222-2222-2222-2222-222222222222",
		},
	)

	response, err := handler.RevokeSession(
		ctx,
		nil,
	)

	if response != nil {
		t.Fatal(
			"RevokeSession() returned a response, want nil",
		)
	}

	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf(
			"RevokeSession() code = %v, want %v",
			status.Code(err),
			codes.InvalidArgument,
		)
	}

	if authService.revokeSessionCalled {
		t.Fatal(
			"RevokeSession() called auth service for nil request",
		)
	}
}
func TestIdentityHandlerRevokeSessionMapsSessionNotFound(
	t *testing.T,
) {
	authService := &fakeAuthService{
		revokeSessionErr: auth.ErrSessionNotFound,
	}

	handler := NewIdentityHandler(authService)

	ctx := contextWithAuthenticatedPrincipal(
		context.Background(),
		authenticatedPrincipal{
			IdentityID: "11111111-1111-1111-1111-111111111111",
			SessionID:  "22222222-2222-2222-2222-222222222222",
		},
	)

	response, err := handler.RevokeSession(
		ctx,
		&identityv1.RevokeSessionRequest{
			SessionId: "33333333-3333-3333-3333-333333333333",
		},
	)

	if response != nil {
		t.Fatal(
			"RevokeSession() returned a response, want nil",
		)
	}

	if status.Code(err) != codes.NotFound {
		t.Fatalf(
			"RevokeSession() code = %v, want %v",
			status.Code(err),
			codes.NotFound,
		)
	}

	if !authService.revokeSessionCalled {
		t.Fatal(
			"RevokeSession() did not call auth service",
		)
	}
}
func TestIdentityHandlerRevokeSessionMapsUnexpectedErrorToInternal(
	t *testing.T,
) {
	authService := &fakeAuthService{
		revokeSessionErr: errors.New(
			"session revocation failed",
		),
	}

	handler := NewIdentityHandler(authService)

	ctx := contextWithAuthenticatedPrincipal(
		context.Background(),
		authenticatedPrincipal{
			IdentityID: "11111111-1111-1111-1111-111111111111",
			SessionID:  "22222222-2222-2222-2222-222222222222",
		},
	)

	response, err := handler.RevokeSession(
		ctx,
		&identityv1.RevokeSessionRequest{
			SessionId: "33333333-3333-3333-3333-333333333333",
		},
	)

	if response != nil {
		t.Fatal(
			"RevokeSession() returned a response, want nil",
		)
	}

	if status.Code(err) != codes.Internal {
		t.Fatalf(
			"RevokeSession() code = %v, want %v",
			status.Code(err),
			codes.Internal,
		)
	}

	if !authService.revokeSessionCalled {
		t.Fatal(
			"RevokeSession() did not call auth service",
		)
	}
}

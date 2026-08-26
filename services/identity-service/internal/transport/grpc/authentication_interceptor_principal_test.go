package grpc

import (
	"context"
	"testing"

	identityv1 "github.com/7akoom/ride-platform/gen/go/ride/identity/v1"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func TestAuthenticationUnaryInterceptorAddsPrincipalForGetMyIdentity(
	t *testing.T,
) {
	ctx := metadata.NewIncomingContext(
		context.Background(),
		metadata.Pairs(
			"authorization",
			"Bearer valid-token",
		),
	)

	interceptor := NewAuthenticationUnaryInterceptor(
		func(
			ctx context.Context,
			rawToken string,
		) (string, string, error) {
			if rawToken != "valid-token" {
				t.Fatalf(
					"raw token = %q, expected %q",
					rawToken,
					"valid-token",
				)
			}

			return "identity-123", "session-456", nil
		},
	)

	handlerCalled := false

	_, err := interceptor(
		ctx,
		nil,
		&googlegrpc.UnaryServerInfo{
			FullMethod: identityv1.IdentityService_GetMyIdentity_FullMethodName,
		},
		func(
			ctx context.Context,
			request any,
		) (any, error) {
			handlerCalled = true

			principal, ok :=
				authenticatedPrincipalFromContext(ctx)
			if !ok {
				t.Fatal(
					"authenticated principal is missing from context",
				)
			}

			if principal.IdentityID != "identity-123" {
				t.Fatalf(
					"identity ID = %q, expected %q",
					principal.IdentityID,
					"identity-123",
				)
			}

			if principal.SessionID != "session-456" {
				t.Fatalf(
					"session ID = %q, expected %q",
					principal.SessionID,
					"session-456",
				)
			}

			return nil, nil
		},
	)

	if err != nil {
		t.Fatalf(
			"interceptor returned an error: %v",
			err,
		)
	}

	if !handlerCalled {
		t.Fatal(
			"GetMyIdentity handler was not called",
		)
	}
}
func TestAuthenticationUnaryInterceptorAddsPrincipalToContext(
	t *testing.T,
) {
	ctx := metadata.NewIncomingContext(
		context.Background(),
		metadata.Pairs(
			"authorization",
			"Bearer valid-token",
		),
	)

	interceptor := NewAuthenticationUnaryInterceptor(
		func(
			ctx context.Context,
			rawToken string,
		) (string, string, error) {
			if rawToken != "valid-token" {
				t.Fatalf(
					"raw token = %q, expected %q",
					rawToken,
					"valid-token",
				)
			}

			return "identity-123", "session-456", nil
		},
	)

	handlerCalled := false

	_, err := interceptor(
		ctx,
		nil,
		&googlegrpc.UnaryServerInfo{
			FullMethod: identityv1.IdentityService_RequestIdentifierLinkOTP_FullMethodName,
		},
		func(
			ctx context.Context,
			request any,
		) (any, error) {
			handlerCalled = true

			principal, ok :=
				authenticatedPrincipalFromContext(ctx)
			if !ok {
				t.Fatal(
					"authenticated principal is missing from context",
				)
			}

			if principal.IdentityID != "identity-123" {
				t.Fatalf(
					"identity ID = %q, expected %q",
					principal.IdentityID,
					"identity-123",
				)
			}

			if principal.SessionID != "session-456" {
				t.Fatalf(
					"session ID = %q, expected %q",
					principal.SessionID,
					"session-456",
				)
			}

			return nil, nil
		},
	)
	if err != nil {
		t.Fatalf(
			"interceptor returned error: %v",
			err,
		)
	}

	if !handlerCalled {
		t.Fatal(
			"handler was not called for authenticated request",
		)
	}
}

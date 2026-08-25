package grpc

import (
	"context"
	"errors"
	"testing"

	identityv1 "github.com/7akoom/ride-platform/gen/go/ride/identity/v1"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestAuthenticationUnaryInterceptorAllowsPublicMethodWithoutToken(
	t *testing.T,
) {
	verifierCalled := false

	interceptor := NewAuthenticationUnaryInterceptor(
		func(
			ctx context.Context,
			rawToken string,
		) (string, string, error) {
			verifierCalled = true

			return "", "", nil
		},
	)

	handlerCalled := false

	_, err := interceptor(
		context.Background(),
		nil,
		&googlegrpc.UnaryServerInfo{
			FullMethod: identityv1.IdentityService_RequestLoginOTP_FullMethodName,
		},
		func(
			ctx context.Context,
			request any,
		) (any, error) {
			handlerCalled = true

			return nil, nil
		},
	)
	if err != nil {
		t.Fatalf(
			"interceptor returned error: %v",
			err,
		)
	}

	if verifierCalled {
		t.Fatal(
			"access token verifier was called for public method",
		)
	}

	if !handlerCalled {
		t.Fatal(
			"handler was not called for public method",
		)
	}
}

func TestAuthenticationUnaryInterceptorRejectsProtectedMethodWithoutToken(
	t *testing.T,
) {
	interceptor := NewAuthenticationUnaryInterceptor(
		func(
			ctx context.Context,
			rawToken string,
		) (string, string, error) {
			return "", "", nil
		},
	)

	_, err := interceptor(
		context.Background(),
		nil,
		&googlegrpc.UnaryServerInfo{
			FullMethod: identityv1.IdentityService_RequestIdentifierLinkOTP_FullMethodName,
		},
		func(
			ctx context.Context,
			request any,
		) (any, error) {
			t.Fatal(
				"handler was called without authentication",
			)

			return nil, nil
		},
	)

	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf(
			"error code = %s, expected %s",
			status.Code(err),
			codes.Unauthenticated,
		)
	}
}

func TestAuthenticationUnaryInterceptorRejectsIdentifierUnlinkRequestWithoutToken(
	t *testing.T,
) {
	interceptor := NewAuthenticationUnaryInterceptor(
		func(
			ctx context.Context,
			rawToken string,
		) (string, string, error) {
			t.Fatal(
				"access token verifier was called without a token",
			)

			return "", "", nil
		},
	)

	_, err := interceptor(
		context.Background(),
		nil,
		&googlegrpc.UnaryServerInfo{
			FullMethod: identityv1.IdentityService_RequestIdentifierUnlinkOTP_FullMethodName,
		},
		func(
			ctx context.Context,
			request any,
		) (any, error) {
			t.Fatal(
				"RequestIdentifierUnlinkOTP handler was called without authentication",
			)

			return nil, nil
		},
	)

	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf(
			"error code = %s, expected %s",
			status.Code(err),
			codes.Unauthenticated,
		)
	}
}

func TestAuthenticationUnaryInterceptorRejectsIdentifierUnlinkVerificationWithoutToken(
	t *testing.T,
) {
	interceptor := NewAuthenticationUnaryInterceptor(
		func(
			ctx context.Context,
			rawToken string,
		) (string, string, error) {
			t.Fatal(
				"access token verifier was called without a token",
			)

			return "", "", nil
		},
	)

	_, err := interceptor(
		context.Background(),
		nil,
		&googlegrpc.UnaryServerInfo{
			FullMethod: identityv1.IdentityService_VerifyIdentifierUnlinkOTP_FullMethodName,
		},
		func(
			ctx context.Context,
			request any,
		) (any, error) {
			t.Fatal(
				"VerifyIdentifierUnlinkOTP handler was called without authentication",
			)

			return nil, nil
		},
	)

	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf(
			"error code = %s, expected %s",
			status.Code(err),
			codes.Unauthenticated,
		)
	}
}

func TestAuthenticationUnaryInterceptorRejectsGetMyIdentityWithoutToken(
	t *testing.T,
) {
	interceptor := NewAuthenticationUnaryInterceptor(
		func(
			ctx context.Context,
			rawToken string,
		) (string, string, error) {
			t.Fatal(
				"access token verifier was called without a token",
			)

			return "", "", nil
		},
	)

	_, err := interceptor(
		context.Background(),
		nil,
		&googlegrpc.UnaryServerInfo{
			FullMethod: identityv1.IdentityService_GetMyIdentity_FullMethodName,
		},
		func(
			ctx context.Context,
			request any,
		) (any, error) {
			t.Fatal(
				"GetMyIdentity handler was called without authentication",
			)

			return nil, nil
		},
	)

	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf(
			"error code = %s, expected %s",
			status.Code(err),
			codes.Unauthenticated,
		)
	}
}

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

func TestAuthenticationUnaryInterceptorRejectsInvalidToken(
	t *testing.T,
) {
	ctx := metadata.NewIncomingContext(
		context.Background(),
		metadata.Pairs(
			"authorization",
			"Bearer invalid-token",
		),
	)

	interceptor := NewAuthenticationUnaryInterceptor(
		func(
			ctx context.Context,
			rawToken string,
		) (string, string, error) {
			if rawToken != "invalid-token" {
				t.Fatalf(
					"raw token = %q, expected %q",
					rawToken,
					"invalid-token",
				)
			}

			return "", "", errors.New(
				"token verification failed",
			)
		},
	)

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
			t.Fatal(
				"handler was called with invalid token",
			)

			return nil, nil
		},
	)

	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf(
			"error code = %s, expected %s",
			status.Code(err),
			codes.Unauthenticated,
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

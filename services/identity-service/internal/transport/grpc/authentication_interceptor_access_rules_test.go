package grpc

import (
	"context"
	"testing"

	identityv1 "github.com/7akoom/ride-platform/gen/go/ride/identity/v1"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

func TestRequiresAuthenticationAllowsOnlyExplicitPublicMethods(
	t *testing.T,
) {
	publicMethods := []string{
		identityv1.IdentityService_RequestLoginOTP_FullMethodName,
		identityv1.IdentityService_VerifyLoginOTP_FullMethodName,
		identityv1.IdentityService_RefreshToken_FullMethodName,
		identityv1.IdentityService_Logout_FullMethodName,
		identityv1.IdentityService_LogoutAllSessions_FullMethodName,
		healthv1.Health_Check_FullMethodName,
	}

	for _, fullMethod := range publicMethods {
		fullMethod := fullMethod

		t.Run(
			fullMethod,
			func(t *testing.T) {
				if requiresAuthentication(fullMethod) {
					t.Fatalf(
						"method %q unexpectedly requires authentication",
						fullMethod,
					)
				}
			},
		)
	}
}

func TestRequiresAuthenticationProtectsCurrentAuthenticatedMethods(
	t *testing.T,
) {
	protectedMethods := []string{
		identityv1.IdentityService_RequestIdentifierLinkOTP_FullMethodName,
		identityv1.IdentityService_VerifyIdentifierLinkOTP_FullMethodName,
		identityv1.IdentityService_RequestIdentifierUnlinkOTP_FullMethodName,
		identityv1.IdentityService_VerifyIdentifierUnlinkOTP_FullMethodName,
		identityv1.IdentityService_GetMyIdentity_FullMethodName,
		identityv1.IdentityService_ListMySessions_FullMethodName,
		identityv1.IdentityService_RevokeSession_FullMethodName,
	}

	for _, fullMethod := range protectedMethods {
		fullMethod := fullMethod

		t.Run(
			fullMethod,
			func(t *testing.T) {
				if !requiresAuthentication(fullMethod) {
					t.Fatalf(
						"method %q unexpectedly allows unauthenticated access",
						fullMethod,
					)
				}
			},
		)
	}
}

func TestRequiresAuthenticationProtectsUnknownMethodByDefault(
	t *testing.T,
) {
	if !requiresAuthentication(
		"/ride.identity.v1.IdentityService/FutureSensitiveRPC",
	) {
		t.Fatal(
			"unknown method unexpectedly allows unauthenticated access",
		)
	}
}

func TestAuthenticationUnaryInterceptorAllowsPublicMethodWithoutToken(
	t *testing.T,
) {
	verifierCalled := false

	interceptor := NewAuthenticationUnaryInterceptor(
		func(
			ctx context.Context,
			rawToken string,
		) (string, string, string, error) {
			verifierCalled = true

			return "", "", "", nil
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
		) (string, string, string, error) {
			t.Fatal(
				"access token verifier was called without a token",
			)

			return "", "", "", nil
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
				"protected handler was called without authentication",
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

func TestAuthenticationUnaryInterceptorRejectsUnknownMethodWithoutToken(
	t *testing.T,
) {
	interceptor := NewAuthenticationUnaryInterceptor(
		func(
			ctx context.Context,
			rawToken string,
		) (string, string, string, error) {
			t.Fatal(
				"access token verifier was called without a token",
			)

			return "", "", "", nil
		},
	)

	_, err := interceptor(
		context.Background(),
		nil,
		&googlegrpc.UnaryServerInfo{
			FullMethod: "/ride.identity.v1.IdentityService/FutureSensitiveRPC",
		},
		func(
			ctx context.Context,
			request any,
		) (any, error) {
			t.Fatal(
				"unknown handler was called without authentication",
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

func TestAuthenticationUnaryInterceptorRejectsMissingMethodInformation(
	t *testing.T,
) {
	interceptor := NewAuthenticationUnaryInterceptor(
		func(
			ctx context.Context,
			rawToken string,
		) (string, string, string, error) {
			return "", "", "", nil
		},
	)

	_, err := interceptor(
		context.Background(),
		nil,
		nil,
		func(
			ctx context.Context,
			request any,
		) (any, error) {
			t.Fatal(
				"handler was called without method information",
			)

			return nil, nil
		},
	)

	if status.Code(err) != codes.Internal {
		t.Fatalf(
			"error code = %s, expected %s",
			status.Code(err),
			codes.Internal,
		)
	}
}

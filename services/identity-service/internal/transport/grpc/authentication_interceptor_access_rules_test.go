package grpc

import (
	"context"
	"testing"

	identityv1 "github.com/7akoom/ride-platform/gen/go/ride/identity/v1"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
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

func TestAuthenticationUnaryInterceptorRejectsListMySessionsWithoutToken(
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
			FullMethod: identityv1.IdentityService_ListMySessions_FullMethodName,
		},
		func(
			ctx context.Context,
			request any,
		) (any, error) {
			t.Fatal(
				"ListMySessions handler was called without authentication",
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

func TestAuthenticationUnaryInterceptorRejectsRevokeSessionWithoutToken(
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
			FullMethod: identityv1.IdentityService_RevokeSession_FullMethodName,
		},
		func(
			ctx context.Context,
			request any,
		) (any, error) {
			t.Fatal(
				"RevokeSession handler was called without authentication",
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

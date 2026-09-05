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
		) (string, string, string, error) {
			if rawToken != "invalid-token" {
				t.Fatalf(
					"raw token = %q, expected %q",
					rawToken,
					"invalid-token",
				)
			}

			return "", "", "", errors.New(
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

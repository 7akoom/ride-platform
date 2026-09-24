package grpc

import (
	"context"
	"crypto/subtle"
	"strings"

	identityv1 "github.com/7akoom/ride-platform/gen/go/ride/identity/v1"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// internalMethods are called by other services only, with the shared
// internal service token instead of a person's access token. They have no
// route through the gateway, and the gateway refuses the internal token.
var internalMethods = map[string]struct{}{
	identityv1.WalletPinService_VerifyWalletPin_FullMethodName:             {},
	identityv1.IdentityDirectoryService_FindIdentityByPhone_FullMethodName: {},
	identityv1.IdentityDirectoryService_GetIdentityPhone_FullMethodName:    {},
}

func isInternalMethod(fullMethod string) bool {
	_, internal := internalMethods[fullMethod]

	return internal
}

type internalCallContextKey struct{}

// NewInternalServiceUnaryInterceptor lets the internal methods through only
// with the internal service token, and marks the call so their handlers can
// check it came this way (they refuse any call without the mark). Other
// methods pass untouched.
func NewInternalServiceUnaryInterceptor(internalServiceToken string) googlegrpc.UnaryServerInterceptor {
	internalServiceToken = strings.TrimSpace(internalServiceToken)
	if internalServiceToken == "" {
		panic("internal service token is required")
	}

	return func(
		ctx context.Context,
		request any,
		info *googlegrpc.UnaryServerInfo,
		handler googlegrpc.UnaryHandler,
	) (any, error) {
		if info == nil || !isInternalMethod(info.FullMethod) {
			return handler(ctx, request)
		}

		token, err := bearerTokenFromIncomingContext(ctx)
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "the internal service token is required")
		}

		if subtle.ConstantTimeCompare([]byte(token), []byte(internalServiceToken)) != 1 {
			return nil, status.Error(codes.PermissionDenied, "permission denied")
		}

		return handler(context.WithValue(ctx, internalCallContextKey{}, true), request)
	}
}

// requireInternalCall fails closed: a handler of an internal method runs
// only for a call the internal service interceptor let through.
func requireInternalCall(ctx context.Context) error {
	if internal, _ := ctx.Value(internalCallContextKey{}).(bool); internal {
		return nil
	}

	return status.Error(codes.PermissionDenied, "permission denied")
}

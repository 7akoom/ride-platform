package grpc

import (
	"context"
	"crypto/subtle"
	"errors"
	"strings"

	"github.com/7akoom/ride-platform/services/notification-service/internal/infrastructure/token"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// internalServicePrincipalID marks a call authenticated via the shared
// internal-service token rather than an end-user access token — e.g. a
// background NATS consumer (no end-user request in flight to derive a
// token from) or another service acting on its own behalf.
const internalServicePrincipalID = "internal-service"

// NewAuthenticationUnaryInterceptor rejects any request without a valid
// credential, except the health check. It accepts two kinds of
// credential in the same Authorization header: an end-user access token
// issued by identity-service, OR the shared internal-service token used
// for service-to-service calls (see internal/config's
// AccessToken*/InternalServiceToken fields). This is authentication only
// (verifying WHO/WHAT is calling), not authorization (deciding WHAT
// they're allowed to do) — a handler that needs to restrict a call to
// its own resource owner reads the principal via
// authenticatedPrincipalFromContext and checks it itself.
func NewAuthenticationUnaryInterceptor(
	verifier *token.AccessTokenVerifier,
	internalServiceToken string,
) googlegrpc.UnaryServerInterceptor {
	if verifier == nil {
		panic("access token verifier is required")
	}

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
		if info == nil {
			return nil, status.Error(
				codes.Internal,
				"gRPC method information is required",
			)
		}

		if info.FullMethod == healthv1.Health_Check_FullMethodName {
			return handler(ctx, request)
		}

		rawToken, err := bearerTokenFromIncomingContext(ctx)
		if err != nil {
			return nil, status.Error(
				codes.Unauthenticated,
				"valid access token is required",
			)
		}

		if subtle.ConstantTimeCompare(
			[]byte(rawToken),
			[]byte(internalServiceToken),
		) == 1 {
			return handler(
				contextWithAuthenticatedPrincipal(
					ctx,
					authenticatedPrincipal{
						IdentityID: internalServicePrincipalID,
						SessionID:  internalServicePrincipalID,
					},
				),
				request,
			)
		}

		claims, err := verifier.Verify(rawToken)
		if err != nil {
			return nil, status.Error(
				codes.Unauthenticated,
				"access token is invalid",
			)
		}

		identityID := strings.TrimSpace(claims.Subject)
		sessionID := strings.TrimSpace(claims.SessionID)

		if identityID == "" || sessionID == "" {
			return nil, status.Error(
				codes.Unauthenticated,
				"access token is invalid",
			)
		}

		authenticatedContext := contextWithAuthenticatedPrincipal(
			ctx,
			authenticatedPrincipal{
				IdentityID: identityID,
				SessionID:  sessionID,
				TenantHint: strings.TrimSpace(claims.TenantHint),
			},
		)

		return handler(authenticatedContext, request)
	}
}

func bearerTokenFromIncomingContext(ctx context.Context) (string, error) {
	incomingMetadata, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", errors.New("gRPC metadata is missing")
	}

	authorizationValues := incomingMetadata.Get("authorization")

	if len(authorizationValues) != 1 {
		return "", errors.New(
			"authorization metadata must contain exactly one value",
		)
	}

	authorizationParts := strings.Fields(authorizationValues[0])

	if len(authorizationParts) != 2 ||
		!strings.EqualFold(authorizationParts[0], "Bearer") {
		return "", errors.New("authorization metadata is invalid")
	}

	rawToken := strings.TrimSpace(authorizationParts[1])
	if rawToken == "" {
		return "", errors.New("access token is empty")
	}

	return rawToken, nil
}

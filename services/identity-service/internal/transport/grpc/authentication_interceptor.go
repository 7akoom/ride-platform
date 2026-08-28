package grpc

import (
	"context"
	"errors"
	"strings"

	identityv1 "github.com/7akoom/ride-platform/gen/go/ride/identity/v1"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type AccessTokenVerifyFunc func(
	ctx context.Context,
	rawToken string,
) (
	identityID string,
	sessionID string,
	tenantHint string,
	err error,
)

func NewAuthenticationUnaryInterceptor(
	verifyAccessToken AccessTokenVerifyFunc,
) googlegrpc.UnaryServerInterceptor {
	if verifyAccessToken == nil {
		panic("access token verifier is required")
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

		if !requiresAuthentication(info.FullMethod) {
			return handler(
				ctx,
				request,
			)
		}

		rawToken, err := bearerTokenFromIncomingContext(ctx)
		if err != nil {
			return nil, status.Error(
				codes.Unauthenticated,
				"valid access token is required",
			)
		}

		identityID, sessionID, tenantHint, err :=
			verifyAccessToken(
				ctx,
				rawToken,
			)
		if err != nil {
			return nil, status.Error(
				codes.Unauthenticated,
				"access token is invalid",
			)
		}

		identityID = strings.TrimSpace(identityID)
		sessionID = strings.TrimSpace(sessionID)
		tenantHint = strings.TrimSpace(tenantHint)

		if identityID == "" || sessionID == "" {
			return nil, status.Error(
				codes.Unauthenticated,
				"access token is invalid",
			)
		}

		authenticatedContext :=
			contextWithAuthenticatedPrincipal(
				ctx,
				authenticatedPrincipal{
					IdentityID: identityID,
					SessionID:  sessionID,
					TenantHint: tenantHint,
				},
			)

		return handler(
			authenticatedContext,
			request,
		)
	}
}

func requiresAuthentication(
	fullMethod string,
) bool {
	switch fullMethod {
	case identityv1.IdentityService_RequestIdentifierLinkOTP_FullMethodName,
		identityv1.IdentityService_VerifyIdentifierLinkOTP_FullMethodName,
		identityv1.IdentityService_RequestIdentifierUnlinkOTP_FullMethodName,
		identityv1.IdentityService_VerifyIdentifierUnlinkOTP_FullMethodName,
		identityv1.IdentityService_GetMyIdentity_FullMethodName,
		identityv1.IdentityService_ListMySessions_FullMethodName,
		identityv1.IdentityService_RevokeSession_FullMethodName:
		return true

	default:
		return false
	}
}

func bearerTokenFromIncomingContext(
	ctx context.Context,
) (string, error) {
	incomingMetadata, ok :=
		metadata.FromIncomingContext(ctx)
	if !ok {
		return "", errors.New(
			"gRPC metadata is missing",
		)
	}

	authorizationValues :=
		incomingMetadata.Get("authorization")

	if len(authorizationValues) != 1 {
		return "", errors.New(
			"authorization metadata must contain exactly one value",
		)
	}

	authorizationParts :=
		strings.Fields(
			authorizationValues[0],
		)

	if len(authorizationParts) != 2 ||
		!strings.EqualFold(
			authorizationParts[0],
			"Bearer",
		) {
		return "", errors.New(
			"authorization metadata is invalid",
		)
	}

	rawToken := strings.TrimSpace(
		authorizationParts[1],
	)
	if rawToken == "" {
		return "", errors.New(
			"access token is empty",
		)
	}

	return rawToken, nil
}

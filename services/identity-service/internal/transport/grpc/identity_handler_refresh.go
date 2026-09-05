package grpc

import (
	"context"
	"errors"
	"strings"

	identityv1 "github.com/7akoom/ride-platform/gen/go/ride/identity/v1"
	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (h *IdentityHandler) RefreshToken(
	ctx context.Context,
	request *identityv1.RefreshTokenRequest,
) (*identityv1.RefreshTokenResponse, error) {
	if request == nil {
		return nil, status.Error(
			codes.InvalidArgument,
			"request is required",
		)
	}

	refreshToken := request.GetRefreshToken()

	if strings.TrimSpace(refreshToken) == "" {
		return nil, status.Error(
			codes.InvalidArgument,
			"refresh token is required",
		)
	}

	result, err := h.authService.RefreshToken(
		ctx,
		auth.RefreshTokenInput{
			RefreshToken: refreshToken,
		},
	)
	if err != nil {
		switch {
		case errors.Is(
			err,
			auth.ErrInvalidRefreshToken,
		),
			errors.Is(
				err,
				auth.ErrRefreshTokenExpired,
			),
			errors.Is(
				err,
				auth.ErrRefreshTokenRevoked,
			),
			errors.Is(
				err,
				auth.ErrRefreshTokenReused,
			),
			errors.Is(
				err,
				auth.ErrSessionExpired,
			),
			errors.Is(
				err,
				auth.ErrSessionRevoked,
			):
			return nil, status.Error(
				codes.Unauthenticated,
				"refresh token is no longer valid",
			)

		case errors.Is(
			err,
			auth.ErrIdentityInactive,
		):
			return nil, status.Error(
				codes.PermissionDenied,
				"identity is inactive",
			)

		default:
			return nil, status.Error(
				codes.Internal,
				"failed to refresh token",
			)
		}
	}

	return &identityv1.RefreshTokenResponse{
		IdentityId:                  result.IdentityID,
		AccessToken:                 result.AccessToken,
		RefreshToken:                result.RefreshToken,
		AccessTokenExpiresInSeconds: result.AccessTokenExpiresInSeconds,
	}, nil
}

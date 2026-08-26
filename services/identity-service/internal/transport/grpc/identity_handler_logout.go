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

func (h *IdentityHandler) Logout(
	ctx context.Context,
	request *identityv1.LogoutRequest,
) (*identityv1.LogoutResponse, error) {
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

	err := h.authService.Logout(
		ctx,
		auth.LogoutInput{
			RefreshToken: refreshToken,
		},
	)
	if err != nil {
		if errors.Is(
			err,
			auth.ErrInvalidRefreshToken,
		) {
			return nil, status.Error(
				codes.InvalidArgument,
				"invalid refresh token",
			)
		}

		return nil, status.Error(
			codes.Internal,
			"failed to log out",
		)
	}

	return &identityv1.LogoutResponse{}, nil
}

func (h *IdentityHandler) LogoutAllSessions(
	ctx context.Context,
	request *identityv1.LogoutAllSessionsRequest,
) (*identityv1.LogoutAllSessionsResponse, error) {
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

	err := h.authService.LogoutAllSessions(
		ctx,
		auth.LogoutAllSessionsInput{
			RefreshToken: refreshToken,
		},
	)
	if err != nil {
		if errors.Is(
			err,
			auth.ErrInvalidRefreshToken,
		) {
			return nil, status.Error(
				codes.InvalidArgument,
				"invalid refresh token",
			)
		}

		return nil, status.Error(
			codes.Internal,
			"failed to log out all sessions",
		)
	}

	return &identityv1.LogoutAllSessionsResponse{}, nil
}

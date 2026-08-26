package grpc

import (
	"context"
	"errors"
	"strings"

	identityv1 "github.com/7akoom/ride-platform/gen/go/ride/identity/v1"
	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (h *IdentityHandler) ListMySessions(
	ctx context.Context,
	request *identityv1.ListMySessionsRequest,
) (*identityv1.ListMySessionsResponse, error) {
	if request == nil {
		return nil, status.Error(
			codes.InvalidArgument,
			"request is required",
		)
	}

	principal, ok := authenticatedPrincipalFromContext(ctx)
	if !ok {
		return nil, status.Error(
			codes.Unauthenticated,
			"authenticated identity is required",
		)
	}

	result, err := h.authService.ListMySessions(
		ctx,
		auth.ListMySessionsInput{
			IdentityID:       principal.IdentityID,
			CurrentSessionID: principal.SessionID,
		},
	)
	if err != nil {
		return nil, status.Error(
			codes.Internal,
			"failed to list authentication sessions",
		)
	}

	sessions := make(
		[]*identityv1.Session,
		0,
		len(result.Sessions),
	)

	for _, item := range result.Sessions {
		var lastSeenAt *timestamppb.Timestamp

		if item.LastSeenAt != nil {
			lastSeenAt = timestamppb.New(
				*item.LastSeenAt,
			)
		}

		sessions = append(
			sessions,
			&identityv1.Session{
				SessionId:  item.SessionID,
				ClientId:   item.ClientID,
				DeviceId:   item.DeviceID,
				DeviceName: item.DeviceName,
				Platform:   item.Platform,
				AppVersion: item.AppVersion,
				IpAddress:  item.IPAddress,
				UserAgent:  item.UserAgent,
				ExpiresAt: timestamppb.New(
					item.ExpiresAt,
				),
				LastSeenAt: lastSeenAt,
				CreatedAt: timestamppb.New(
					item.CreatedAt,
				),
				IsCurrent: item.IsCurrent,
			},
		)
	}

	return &identityv1.ListMySessionsResponse{
		Sessions: sessions,
	}, nil
}

func (h *IdentityHandler) RevokeSession(
	ctx context.Context,
	request *identityv1.RevokeSessionRequest,
) (*identityv1.RevokeSessionResponse, error) {
	if request == nil {
		return nil, status.Error(
			codes.InvalidArgument,
			"request is required",
		)
	}

	sessionID := strings.TrimSpace(request.GetSessionId())
	if sessionID == "" {
		return nil, status.Error(
			codes.InvalidArgument,
			"session ID is required",
		)
	}

	principal, ok := authenticatedPrincipalFromContext(ctx)
	if !ok {
		return nil, status.Error(
			codes.Unauthenticated,
			"authenticated identity is required",
		)
	}

	err := h.authService.RevokeSession(
		ctx,
		auth.RevokeSessionInput{
			IdentityID: principal.IdentityID,
			SessionID:  sessionID,
		},
	)
	if err != nil {
		if errors.Is(err, auth.ErrSessionNotFound) {
			return nil, status.Error(
				codes.NotFound,
				"authentication session not found",
			)
		}

		return nil, status.Error(
			codes.Internal,
			"failed to revoke authentication session",
		)
	}

	return &identityv1.RevokeSessionResponse{}, nil
}

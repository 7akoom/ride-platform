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

type IdentityHandler struct {
	identityv1.UnimplementedIdentityServiceServer

	authService auth.Service
}

func NewIdentityHandler(
	authService auth.Service,
) *IdentityHandler {
	return &IdentityHandler{
		authService: authService,
	}
}

func (h *IdentityHandler) RequestOTP(
	ctx context.Context,
	request *identityv1.RequestOTPRequest,
) (*identityv1.RequestOTPResponse, error) {
	if request == nil {
		return nil, status.Error(
			codes.InvalidArgument,
			"request is required",
		)
	}

	phoneNumber := strings.TrimSpace(
		request.GetPhoneNumber(),
	)

	if phoneNumber == "" {
		return nil, status.Error(
			codes.InvalidArgument,
			"phone number is required",
		)
	}

	result, err := h.authService.RequestOTP(
		ctx,
		auth.RequestOTPInput{
			PhoneNumber: phoneNumber,
		},
	)
	if err != nil {
		if errors.Is(
			err,
			auth.ErrInvalidPhoneNumber,
		) {
			return nil, status.Error(
				codes.InvalidArgument,
				"invalid phone number",
			)
		}

		if errors.Is(
			err,
			auth.ErrOTPRequestRateLimited,
		) {
			return nil, status.Error(
				codes.ResourceExhausted,
				"OTP request rate limit exceeded",
			)
		}

		return nil, status.Error(
			codes.Internal,
			"failed to request OTP",
		)
	}

	return &identityv1.RequestOTPResponse{
		ChallengeId:      result.ChallengeID,
		ExpiresInSeconds: result.ExpiresInSeconds,
	}, nil
}

func (h *IdentityHandler) VerifyOTP(
	ctx context.Context,
	request *identityv1.VerifyOTPRequest,
) (*identityv1.VerifyOTPResponse, error) {
	if request == nil {
		return nil, status.Error(
			codes.InvalidArgument,
			"request is required",
		)
	}

	challengeID := strings.TrimSpace(
		request.GetChallengeId(),
	)

	if challengeID == "" {
		return nil, status.Error(
			codes.InvalidArgument,
			"challenge ID is required",
		)
	}

	code := strings.TrimSpace(
		request.GetCode(),
	)

	if code == "" {
		return nil, status.Error(
			codes.InvalidArgument,
			"OTP code is required",
		)
	}

	result, err := h.authService.VerifyOTP(
		ctx,
		auth.VerifyOTPInput{
			ChallengeID: challengeID,
			Code:        code,
		},
	)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrChallengeNotFound):
			return nil, status.Error(
				codes.NotFound,
				"OTP challenge not found",
			)

		case errors.Is(err, auth.ErrChallengeExpired):
			return nil, status.Error(
				codes.FailedPrecondition,
				"OTP challenge expired",
			)

		case errors.Is(err, auth.ErrChallengeUsed):
			return nil, status.Error(
				codes.FailedPrecondition,
				"OTP challenge already used",
			)

			case errors.Is(
				err,
				auth.ErrChallengeCancelled,
			):
				return nil, status.Error(
					codes.FailedPrecondition,
					"OTP challenge cancelled",
				)
		case errors.Is(
				err,
				auth.ErrChallengeAttemptsExceeded,
			):
				return nil, status.Error(
					codes.ResourceExhausted,
					"OTP challenge attempts exceeded",
				)

		case errors.Is(err, auth.ErrInvalidOTP):
			return nil, status.Error(
				codes.Unauthenticated,
				"invalid OTP",
			)

		case errors.Is(err, auth.ErrIdentityInactive):
			return nil, status.Error(
				codes.PermissionDenied,
				"identity is inactive",
			)

		default:
			return nil, status.Error(
				codes.Internal,
				"failed to verify OTP",
			)
		}
	}

	return &identityv1.VerifyOTPResponse{
		IdentityId: result.IdentityID,
		AccessToken: result.AccessToken,
		RefreshToken: result.RefreshToken,
		AccessTokenExpiresInSeconds: result.AccessTokenExpiresInSeconds,
	}, nil
}

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

	if refreshToken == "" {
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
		IdentityId: result.IdentityID,
		AccessToken: result.AccessToken,
		RefreshToken: result.RefreshToken,
		AccessTokenExpiresInSeconds: result.AccessTokenExpiresInSeconds,
	}, nil
}
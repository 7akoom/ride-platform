package grpc

import (
	"context"
	"errors"
	"net"
	"strings"

	identityv1 "github.com/7akoom/ride-platform/gen/go/ride/identity/v1"
	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

func (h *IdentityHandler) RequestLoginOTP(
	ctx context.Context,
	request *identityv1.RequestLoginOTPRequest,
) (*identityv1.RequestLoginOTPResponse, error) {
	if request == nil {
		return nil, status.Error(
			codes.InvalidArgument,
			"request is required",
		)
	}

	requestIdentifier := request.GetIdentifier()
	if requestIdentifier == nil {
		return nil, status.Error(
			codes.InvalidArgument,
			"identifier is required",
		)
	}

	var identifierType auth.IdentifierType

	switch requestIdentifier.GetType() {
	case identityv1.IdentifierType_IDENTIFIER_TYPE_PHONE:
		identifierType = auth.IdentifierTypePhone

	case identityv1.IdentifierType_IDENTIFIER_TYPE_EMAIL:
		identifierType = auth.IdentifierTypeEmail

	default:
		return nil, status.Error(
			codes.InvalidArgument,
			"identifier type is required",
		)
	}

	identifierValue := strings.TrimSpace(
		requestIdentifier.GetValue(),
	)
	if identifierValue == "" {
		return nil, status.Error(
			codes.InvalidArgument,
			"identifier value is required",
		)
	}

	result, err := h.authService.RequestOTP(
		ctx,
		auth.RequestOTPInput{
			Identifier: auth.Identifier{
				Type:  identifierType,
				Value: identifierValue,
			},
			Purpose: auth.OTPPurposeLogin,
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
			auth.ErrInvalidEmailAddress,
		) {
			return nil, status.Error(
				codes.InvalidArgument,
				"invalid email address",
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
			"failed to request login OTP",
		)
	}

	return &identityv1.RequestLoginOTPResponse{
		ChallengeId:      result.ChallengeID,
		ExpiresInSeconds: result.ExpiresInSeconds,
	}, nil
}

func (h *IdentityHandler) VerifyLoginOTP(
	ctx context.Context,
	request *identityv1.VerifyLoginOTPRequest,
) (*identityv1.VerifyLoginOTPResponse, error) {
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
			ChallengeID:     challengeID,
			Code:            code,
			ExpectedPurpose: auth.OTPPurposeLogin,
			SessionMetadata: auth.SessionMetadata{
				ClientID: strings.TrimSpace(
					request.GetClientId(),
				),
				DeviceID: strings.TrimSpace(
					request.GetDeviceId(),
				),
				DeviceName: strings.TrimSpace(
					request.GetDeviceName(),
				),
				Platform: strings.TrimSpace(
					request.GetPlatform(),
				),
				AppVersion: strings.TrimSpace(
					request.GetAppVersion(),
				),
				IPAddress: sessionIPAddressFromContext(ctx),
				UserAgent: sessionUserAgentFromContext(ctx),
			},
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

		case errors.Is(err, auth.ErrOTPPurposeMismatch):
			return nil, status.Error(
				codes.FailedPrecondition,
				"OTP challenge is not valid for login",
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
				"failed to verify login OTP",
			)
		}
	}

	return &identityv1.VerifyLoginOTPResponse{
		IdentityId:                  result.IdentityID,
		AccessToken:                 result.AccessToken,
		RefreshToken:                result.RefreshToken,
		AccessTokenExpiresInSeconds: result.AccessTokenExpiresInSeconds,
	}, nil
}

func sessionIPAddressFromContext(
	ctx context.Context,
) string {
	peerInfo, ok := peer.FromContext(ctx)
	if !ok || peerInfo.Addr == nil {
		return ""
	}

	address := strings.TrimSpace(
		peerInfo.Addr.String(),
	)
	if address == "" {
		return ""
	}

	host := address

	if splitHost, _, err := net.SplitHostPort(address); err == nil {
		host = splitHost
	}

	host = strings.Trim(
		strings.TrimSpace(host),
		"[]",
	)

	if net.ParseIP(host) == nil {
		return ""
	}

	return host
}

func sessionUserAgentFromContext(
	ctx context.Context,
) string {
	incomingMetadata, ok :=
		metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}

	values := incomingMetadata.Get(
		"user-agent",
	)
	if len(values) == 0 {
		return ""
	}

	return strings.TrimSpace(
		values[0],
	)
}

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

type IdentityHandler struct {
	identityv1.UnimplementedIdentityServiceServer

	authService auth.Service
}

func NewIdentityHandler(
	authService auth.Service,
) *IdentityHandler {
	if authService == nil {
		panic("auth service is required")
	}

	return &IdentityHandler{
		authService: authService,
	}
}

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

func (h *IdentityHandler) RequestIdentifierLinkOTP(
	ctx context.Context,
	request *identityv1.RequestIdentifierLinkOTPRequest,
) (*identityv1.RequestIdentifierLinkOTPResponse, error) {
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

	targetIdentityID := principal.IdentityID

	result, err := h.authService.RequestOTP(
		ctx,
		auth.RequestOTPInput{
			Identifier: auth.Identifier{
				Type:  identifierType,
				Value: identifierValue,
			},
			Purpose:          auth.OTPPurposeLinkIdentifier,
			TargetIdentityID: &targetIdentityID,
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
			"failed to request identifier link OTP",
		)
	}

	return &identityv1.RequestIdentifierLinkOTPResponse{
		ChallengeId:      result.ChallengeID,
		ExpiresInSeconds: result.ExpiresInSeconds,
	}, nil
}

func (h *IdentityHandler) VerifyIdentifierLinkOTP(
	ctx context.Context,
	request *identityv1.VerifyIdentifierLinkOTPRequest,
) (*identityv1.VerifyIdentifierLinkOTPResponse, error) {
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

	targetIdentityID := principal.IdentityID

	_, err := h.authService.VerifyOTP(
		ctx,
		auth.VerifyOTPInput{
			ChallengeID:              challengeID,
			Code:                     code,
			ExpectedPurpose:          auth.OTPPurposeLinkIdentifier,
			ExpectedTargetIdentityID: &targetIdentityID,
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

		case errors.Is(
			err,
			auth.ErrOTPPurposeMismatch,
		),
			errors.Is(
				err,
				auth.ErrOTPChallengeTargetMismatch,
			):
			return nil, status.Error(
				codes.FailedPrecondition,
				"OTP challenge is not valid for identifier linking",
			)

		case errors.Is(err, auth.ErrInvalidOTP):
			return nil, status.Error(
				codes.Unauthenticated,
				"invalid OTP",
			)

		case errors.Is(
			err,
			auth.ErrIdentifierAlreadyLinked,
		):
			return nil, status.Error(
				codes.AlreadyExists,
				"identifier is already linked",
			)

		case errors.Is(err, auth.ErrIdentityInactive):
			return nil, status.Error(
				codes.PermissionDenied,
				"identity is inactive",
			)

		default:
			return nil, status.Error(
				codes.Internal,
				"failed to verify identifier link OTP",
			)
		}
	}

	return &identityv1.VerifyIdentifierLinkOTPResponse{}, nil
}

func (h *IdentityHandler) RequestIdentifierUnlinkOTP(
	ctx context.Context,
	request *identityv1.RequestIdentifierUnlinkOTPRequest,
) (*identityv1.RequestIdentifierUnlinkOTPResponse, error) {
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

	result, err := h.authService.RequestIdentifierUnlinkOTP(
		ctx,
		auth.RequestIdentifierUnlinkOTPInput{
			IdentityID: principal.IdentityID,
			TargetIdentifier: auth.Identifier{
				Type:  identifierType,
				Value: identifierValue,
			},
		},
	)
	if err != nil {
		switch {
		case errors.Is(
			err,
			auth.ErrInvalidPhoneNumber,
		):
			return nil, status.Error(
				codes.InvalidArgument,
				"invalid phone number",
			)

		case errors.Is(
			err,
			auth.ErrInvalidEmailAddress,
		):
			return nil, status.Error(
				codes.InvalidArgument,
				"invalid email address",
			)

		case errors.Is(
			err,
			auth.ErrIdentityNotFound,
		):
			return nil, status.Error(
				codes.NotFound,
				"identity not found",
			)

		case errors.Is(
			err,
			auth.ErrIdentifierNotLinked,
		):
			return nil, status.Error(
				codes.FailedPrecondition,
				"identifier is not linked",
			)

		case errors.Is(
			err,
			auth.ErrLastIdentifierRemoval,
		):
			return nil, status.Error(
				codes.FailedPrecondition,
				"cannot remove the last identity identifier",
			)

		case errors.Is(
			err,
			auth.ErrOTPRequestRateLimited,
		):
			return nil, status.Error(
				codes.ResourceExhausted,
				"OTP request rate limit exceeded",
			)

		default:
			return nil, status.Error(
				codes.Internal,
				"failed to request identifier unlink OTP",
			)
		}
	}

	return &identityv1.RequestIdentifierUnlinkOTPResponse{
		ChallengeId:      result.ChallengeID,
		ExpiresInSeconds: result.ExpiresInSeconds,
	}, nil
}

func (h *IdentityHandler) VerifyIdentifierUnlinkOTP(
	ctx context.Context,
	request *identityv1.VerifyIdentifierUnlinkOTPRequest,
) (*identityv1.VerifyIdentifierUnlinkOTPResponse, error) {
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

	targetIdentityID := principal.IdentityID

	_, err := h.authService.VerifyOTP(
		ctx,
		auth.VerifyOTPInput{
			ChallengeID:              challengeID,
			Code:                     code,
			ExpectedPurpose:          auth.OTPPurposeUnlinkIdentifier,
			ExpectedTargetIdentityID: &targetIdentityID,
		},
	)
	if err != nil {
		switch {
		case errors.Is(
			err,
			auth.ErrChallengeNotFound,
		):
			return nil, status.Error(
				codes.NotFound,
				"OTP challenge not found",
			)

		case errors.Is(
			err,
			auth.ErrChallengeExpired,
		):
			return nil, status.Error(
				codes.FailedPrecondition,
				"OTP challenge expired",
			)

		case errors.Is(
			err,
			auth.ErrChallengeUsed,
		):
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

		case errors.Is(
			err,
			auth.ErrOTPPurposeMismatch,
		),
			errors.Is(
				err,
				auth.ErrOTPChallengeTargetMismatch,
			):
			return nil, status.Error(
				codes.FailedPrecondition,
				"OTP challenge is not valid for identifier unlinking",
			)

		case errors.Is(
			err,
			auth.ErrInvalidOTP,
		):
			return nil, status.Error(
				codes.Unauthenticated,
				"invalid OTP",
			)

		case errors.Is(
			err,
			auth.ErrIdentityNotFound,
		):
			return nil, status.Error(
				codes.NotFound,
				"identity not found",
			)

		case errors.Is(
			err,
			auth.ErrIdentifierNotLinked,
		):
			return nil, status.Error(
				codes.FailedPrecondition,
				"identifier is not linked",
			)

		case errors.Is(
			err,
			auth.ErrLastIdentifierRemoval,
		):
			return nil, status.Error(
				codes.FailedPrecondition,
				"cannot remove the last identity identifier",
			)

		default:
			return nil, status.Error(
				codes.Internal,
				"failed to verify identifier unlink OTP",
			)
		}
	}

	return &identityv1.VerifyIdentifierUnlinkOTPResponse{}, nil
}

func (h *IdentityHandler) GetMyIdentity(
	ctx context.Context,
	request *identityv1.GetMyIdentityRequest,
) (*identityv1.GetMyIdentityResponse, error) {
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

	result, err := h.authService.GetMyIdentity(
		ctx,
		auth.GetMyIdentityInput{
			IdentityID: principal.IdentityID,
		},
	)
	if err != nil {
		if errors.Is(err, auth.ErrIdentityNotFound) {
			return nil, status.Error(
				codes.NotFound,
				"identity not found",
			)
		}

		return nil, status.Error(
			codes.Internal,
			"failed to get identity",
		)
	}

	var identityStatus identityv1.IdentityStatus

	switch result.Status {
	case auth.IdentityStatusActive:
		identityStatus =
			identityv1.IdentityStatus_IDENTITY_STATUS_ACTIVE

	case auth.IdentityStatusSuspended:
		identityStatus =
			identityv1.IdentityStatus_IDENTITY_STATUS_SUSPENDED

	case auth.IdentityStatusDisabled:
		identityStatus =
			identityv1.IdentityStatus_IDENTITY_STATUS_DISABLED

	default:
		return nil, status.Error(
			codes.Internal,
			"identity has invalid status",
		)
	}

	identifiers := make(
		[]*identityv1.VerifiedIdentifier,
		0,
		len(result.Identifiers),
	)

	for _, item := range result.Identifiers {
		var identifierType identityv1.IdentifierType

		switch item.Identifier.Type {
		case auth.IdentifierTypePhone:
			identifierType =
				identityv1.IdentifierType_IDENTIFIER_TYPE_PHONE

		case auth.IdentifierTypeEmail:
			identifierType =
				identityv1.IdentifierType_IDENTIFIER_TYPE_EMAIL

		default:
			return nil, status.Error(
				codes.Internal,
				"identity has invalid identifier type",
			)
		}

		identifiers = append(
			identifiers,
			&identityv1.VerifiedIdentifier{
				Type:       identifierType,
				Value:      item.Identifier.Value,
				VerifiedAt: timestamppb.New(item.VerifiedAt),
			},
		)
	}

	return &identityv1.GetMyIdentityResponse{
		IdentityId:  result.ID,
		Status:      identityStatus,
		Identifiers: identifiers,
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

package grpc

import (
	"context"
	"errors"

	identityv1 "github.com/7akoom/ride-platform/gen/go/ride/identity/v1"
	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

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

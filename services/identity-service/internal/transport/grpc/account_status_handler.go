package grpc

import (
	"context"
	"errors"
	"log/slog"

	identityv1 "github.com/7akoom/ride-platform/gen/go/ride/identity/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

// AccountStatusHandler suspends and reactivates identities for
// support-service (internal token only).
type AccountStatusHandler struct {
	identityv1.UnimplementedIdentityAccountServiceServer

	lifecycle auth.IdentityLifecycleService
	logger    *slog.Logger
}

func NewAccountStatusHandler(lifecycle auth.IdentityLifecycleService, logger *slog.Logger) *AccountStatusHandler {
	if lifecycle == nil {
		panic("identity lifecycle service is required")
	}

	if logger == nil {
		panic("logger is required")
	}

	return &AccountStatusHandler{lifecycle: lifecycle, logger: logger}
}

func (h *AccountStatusHandler) SuspendIdentity(
	ctx context.Context,
	request *identityv1.SuspendIdentityRequest,
) (*identityv1.IdentityAccountStatusResponse, error) {
	return h.transition(ctx, request.GetIdentityId(), h.lifecycle.SuspendIdentity)
}

func (h *AccountStatusHandler) ReactivateIdentity(
	ctx context.Context,
	request *identityv1.ReactivateIdentityRequest,
) (*identityv1.IdentityAccountStatusResponse, error) {
	return h.transition(ctx, request.GetIdentityId(), h.lifecycle.ReactivateIdentity)
}

func (h *AccountStatusHandler) transition(
	ctx context.Context,
	identityID string,
	move func(context.Context, auth.IdentityLifecycleInput) (auth.IdentityLifecycleResult, error),
) (*identityv1.IdentityAccountStatusResponse, error) {
	if err := requireInternalCall(ctx); err != nil {
		return nil, err
	}

	if !looksLikeUUID(identityID) {
		return nil, status.Error(codes.InvalidArgument, "identity_id must be a uuid")
	}

	result, err := move(ctx, auth.IdentityLifecycleInput{IdentityID: identityID})
	if errors.Is(err, auth.ErrIdentityNotFound) {
		return nil, status.Error(codes.NotFound, "identity not found")
	}

	if err != nil {
		h.logger.ErrorContext(ctx, "identity status change failed", "error", err)

		return nil, status.Error(codes.Internal, "internal error")
	}

	return &identityv1.IdentityAccountStatusResponse{
		PreviousStatus: string(result.PreviousStatus),
		CurrentStatus:  string(result.CurrentStatus),
		Changed:        result.Changed,
	}, nil
}

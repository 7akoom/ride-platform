package grpc

import (
	"context"
	"errors"
	"log/slog"
	"time"

	identityv1 "github.com/7akoom/ride-platform/gen/go/ride/identity/v1"
	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	"github.com/7akoom/ride-platform/services/identity-service/internal/application/deletion"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// AccountDeletionHandler serves /v1/me/deletion for the signed-in person.
type AccountDeletionHandler struct {
	identityv1.UnimplementedAccountDeletionServiceServer

	service *deletion.Service
	logger  *slog.Logger
}

func NewAccountDeletionHandler(service *deletion.Service, logger *slog.Logger) *AccountDeletionHandler {
	if service == nil || logger == nil {
		panic("account deletion handler dependencies are required")
	}

	return &AccountDeletionHandler{service: service, logger: logger}
}

func (h *AccountDeletionHandler) GetAccountDeletion(
	ctx context.Context,
	request *identityv1.GetAccountDeletionRequest,
) (*identityv1.AccountDeletionResponse, error) {
	principal, ok := authenticatedPrincipalFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "authenticated identity is required")
	}

	overview, err := h.service.Get(ctx, principal.IdentityID)
	if err != nil {
		return nil, h.mapError(err)
	}

	return &identityv1.AccountDeletionResponse{Deletion: h.toProto(overview.Deletion, overview.Standing)}, nil
}

func (h *AccountDeletionHandler) RequestAccountDeletionOTP(
	ctx context.Context,
	request *identityv1.RequestAccountDeletionOTPRequest,
) (*identityv1.RequestAccountDeletionOTPResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	principal, ok := authenticatedPrincipalFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "authenticated identity is required")
	}

	channel, valid := otpDeliveryChannelFromProto(request.GetDeliveryChannel())
	if !valid {
		return nil, status.Error(codes.InvalidArgument, "invalid OTP delivery channel")
	}

	sourceIPAddress := ""
	if source, ok := requestSourceFromContext(ctx); ok {
		sourceIPAddress = source.IPAddress
	}

	result, err := h.service.RequestOTP(ctx, deletion.RequestOTPInput{
		IdentityID:      principal.IdentityID,
		TenantHint:      principal.TenantHint,
		Channel:         channel,
		Locale:          requestLocaleFromIncomingContext(ctx),
		SourceIPAddress: sourceIPAddress,
	})
	if err != nil {
		return nil, h.mapError(err)
	}

	return &identityv1.RequestAccountDeletionOTPResponse{
		ChallengeId:      result.ChallengeID,
		ExpiresInSeconds: result.ExpiresInSeconds,
	}, nil
}

func (h *AccountDeletionHandler) ConfirmAccountDeletion(
	ctx context.Context,
	request *identityv1.ConfirmAccountDeletionRequest,
) (*identityv1.AccountDeletionResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	principal, ok := authenticatedPrincipalFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "authenticated identity is required")
	}

	if request.GetChallengeId() == "" || request.GetCode() == "" {
		return nil, status.Error(codes.InvalidArgument, "challenge_id and code are required")
	}

	started, err := h.service.Confirm(ctx, deletion.ConfirmRequest{
		IdentityID:        principal.IdentityID,
		ChallengeID:       request.GetChallengeId(),
		Code:              request.GetCode(),
		AcceptBalanceLoss: request.GetAcceptBalanceLoss(),
	})
	if err != nil {
		return nil, h.mapError(err)
	}

	return &identityv1.AccountDeletionResponse{Deletion: h.toProto(started, deletion.Standing{})}, nil
}

func (h *AccountDeletionHandler) toProto(d deletion.Deletion, standing deletion.Standing) *identityv1.AccountDeletion {
	out := &identityv1.AccountDeletion{
		Status:          identityv1.AccountDeletionStatus_ACCOUNT_DELETION_STATUS_NONE,
		GracePeriodDays: int32(h.service.GracePeriod() / (24 * time.Hour)),
	}

	if d.Status == deletion.StatusPending {
		out.Status = identityv1.AccountDeletionStatus_ACCOUNT_DELETION_STATUS_PENDING
		out.RequestedAt = timestamppb.New(d.RequestedAt)
		out.PurgeAfter = timestamppb.New(d.PurgeAfter)
	}

	for _, b := range standing.Blockers {
		out.Blockers = append(out.Blockers, blockerToProto(b))
	}

	for _, balance := range standing.Balances {
		out.ForfeitedBalances = append(out.ForfeitedBalances, &identityv1.ForfeitedBalance{
			OwnerType: balance.OwnerType, Amount: balance.Amount, CurrencyCode: balance.Currency,
		})
	}

	return out
}

func blockerToProto(b deletion.Blocker) identityv1.AccountDeletionBlocker {
	switch b {
	case deletion.BlockerActiveTrip:
		return identityv1.AccountDeletionBlocker_ACCOUNT_DELETION_BLOCKER_ACTIVE_TRIP
	case deletion.BlockerScheduledTrip:
		return identityv1.AccountDeletionBlocker_ACCOUNT_DELETION_BLOCKER_SCHEDULED_TRIP
	case deletion.BlockerUnpaidFees:
		return identityv1.AccountDeletionBlocker_ACCOUNT_DELETION_BLOCKER_UNPAID_FEES
	case deletion.BlockerNegativeBalance:
		return identityv1.AccountDeletionBlocker_ACCOUNT_DELETION_BLOCKER_NEGATIVE_BALANCE
	case deletion.BlockerOpenPayout:
		return identityv1.AccountDeletionBlocker_ACCOUNT_DELETION_BLOCKER_OPEN_PAYOUT
	default:
		return identityv1.AccountDeletionBlocker_ACCOUNT_DELETION_BLOCKER_UNSPECIFIED
	}
}

func (h *AccountDeletionHandler) mapError(err error) error {
	switch {
	case errors.Is(err, deletion.ErrBlocked),
		errors.Is(err, deletion.ErrBalanceNotAccepted),
		errors.Is(err, deletion.ErrAlreadyPending),
		errors.Is(err, deletion.ErrNotActive):
		return status.Error(codes.FailedPrecondition, err.Error())

	case errors.Is(err, auth.ErrInvalidOTP):
		return status.Error(codes.InvalidArgument, "the code is wrong")

	case errors.Is(err, auth.ErrChallengeNotFound),
		errors.Is(err, auth.ErrOTPPurposeMismatch),
		errors.Is(err, auth.ErrOTPChallengeTargetMismatch):
		return status.Error(codes.NotFound, "OTP challenge not found")

	case errors.Is(err, auth.ErrChallengeExpired):
		return status.Error(codes.FailedPrecondition, "OTP challenge expired")

	case errors.Is(err, auth.ErrChallengeUsed):
		return status.Error(codes.FailedPrecondition, "OTP challenge already used")

	case errors.Is(err, auth.ErrChallengeCancelled):
		return status.Error(codes.FailedPrecondition, "OTP challenge cancelled")

	case errors.Is(err, auth.ErrChallengeAttemptsExceeded):
		return status.Error(codes.ResourceExhausted, "too many wrong codes: ask for a new one")

	case errors.Is(err, auth.ErrOTPRequestRateLimited):
		return status.Error(codes.ResourceExhausted, "OTP request rate limit exceeded")

	case errors.Is(err, auth.ErrInvalidOTPDeliveryChannel):
		return status.Error(codes.InvalidArgument, "invalid OTP delivery channel")

	case errors.Is(err, auth.ErrOTPDeliveryChannelUnavailable):
		return status.Error(codes.FailedPrecondition, "OTP delivery channel is unavailable")

	case errors.Is(err, auth.ErrIdentityNotFound):
		return status.Error(codes.NotFound, "identity not found")

	case errors.Is(err, deletion.ErrUnavailable):
		h.logger.Warn("account deletion check failed", "error", err)

		return status.Error(codes.Unavailable, deletion.ErrUnavailable.Error())

	case errors.Is(err, deletion.ErrDeliveryFailed):
		h.logger.Warn("account deletion code delivery failed", "error", err)

		return status.Error(codes.Unavailable, deletion.ErrDeliveryFailed.Error())

	default:
		h.logger.Error("account deletion request failed", "error", err)

		return status.Error(codes.Internal, "failed to process the account deletion")
	}
}

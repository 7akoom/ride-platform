package grpc

import (
	"context"
	"errors"
	"log/slog"
	"time"

	identityv1 "github.com/7akoom/ride-platform/gen/go/ride/identity/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/walletpin"
)

// WalletPinHandler serves the wallet PIN: people, for themselves; wallet-
// service, to verify one.
type WalletPinHandler struct {
	identityv1.UnimplementedWalletPinServiceServer

	pins   *walletpin.Service
	logger *slog.Logger
}

// DirectoryHandler finds people by phone, for wallet-service only.
type DirectoryHandler struct {
	identityv1.UnimplementedIdentityDirectoryServiceServer

	pins *WalletPinHandler
}

// Directory is the directory service over the same wallet PIN service.
func (h *WalletPinHandler) Directory() *DirectoryHandler {
	return &DirectoryHandler{pins: h}
}

func NewWalletPinHandler(pins *walletpin.Service, logger *slog.Logger) *WalletPinHandler {
	if pins == nil {
		panic("wallet PIN service is required")
	}

	if logger == nil {
		panic("logger is required")
	}

	return &WalletPinHandler{pins: pins, logger: logger}
}

func (h *WalletPinHandler) GetWalletPinStatus(
	ctx context.Context,
	_ *identityv1.GetWalletPinStatusRequest,
) (*identityv1.WalletPinStatusResponse, error) {
	principal, ok := authenticatedPrincipalFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "valid access token is required")
	}

	pinStatus, err := h.pins.Status(ctx, principal.IdentityID)
	if err != nil {
		return nil, h.mapError(err)
	}

	return toProtoPinStatus(pinStatus), nil
}

func (h *WalletPinHandler) SetWalletPin(
	ctx context.Context,
	request *identityv1.SetWalletPinRequest,
) (*identityv1.WalletPinStatusResponse, error) {
	principal, ok := authenticatedPrincipalFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "valid access token is required")
	}

	pinStatus, err := h.pins.Set(ctx, walletpin.SetInput{
		IdentityID: principal.IdentityID,
		SessionID:  principal.SessionID,
		NewPIN:     request.GetNewPin(),
		CurrentPIN: request.GetCurrentPin(),
	})
	if err != nil {
		return nil, h.mapError(err)
	}

	return toProtoPinStatus(pinStatus), nil
}

func (h *WalletPinHandler) VerifyWalletPin(
	ctx context.Context,
	request *identityv1.VerifyWalletPinRequest,
) (*identityv1.VerifyWalletPinResponse, error) {
	if err := requireInternalCall(ctx); err != nil {
		return nil, err
	}

	if !looksLikeUUID(request.GetIdentityId()) {
		return &identityv1.VerifyWalletPinResponse{Result: identityv1.PinCheck_PIN_CHECK_NOT_SET}, nil
	}

	result, err := h.pins.Verify(ctx, request.GetIdentityId(), request.GetPin())
	if err != nil {
		return nil, h.mapError(err)
	}

	response := &identityv1.VerifyWalletPinResponse{
		Result:       toProtoPinCheck(result.Check),
		AttemptsLeft: int32(result.AttemptsLeft),
	}

	if result.LockedUntil != nil {
		response.LockedUntil = timestamppb.New(*result.LockedUntil)
	}

	return response, nil
}

func (d *DirectoryHandler) FindIdentityByPhone(
	ctx context.Context,
	request *identityv1.FindIdentityByPhoneRequest,
) (*identityv1.FindIdentityByPhoneResponse, error) {
	if err := requireInternalCall(ctx); err != nil {
		return nil, err
	}

	identityID, found, err := d.pins.pins.FindByPhone(ctx, request.GetPhoneNumber())
	if err != nil {
		return nil, d.pins.mapError(err)
	}

	return &identityv1.FindIdentityByPhoneResponse{Found: found, IdentityId: identityID}, nil
}

func (d *DirectoryHandler) GetIdentityPhone(
	ctx context.Context,
	request *identityv1.GetIdentityPhoneRequest,
) (*identityv1.GetIdentityPhoneResponse, error) {
	if err := requireInternalCall(ctx); err != nil {
		return nil, err
	}

	if !looksLikeUUID(request.GetIdentityId()) {
		return &identityv1.GetIdentityPhoneResponse{}, nil
	}

	phone, err := d.pins.pins.PhoneOf(ctx, request.GetIdentityId())
	if err != nil {
		return nil, d.pins.mapError(err)
	}

	return &identityv1.GetIdentityPhoneResponse{PhoneNumber: phone}, nil
}

func (h *WalletPinHandler) mapError(err error) error {
	switch {
	case errors.Is(err, walletpin.ErrInvalidPIN),
		errors.Is(err, walletpin.ErrWeakPIN),
		errors.Is(err, walletpin.ErrInvalidPhoneNumber),
		errors.Is(err, walletpin.ErrIdentityRequired):
		return status.Error(codes.InvalidArgument, err.Error())

	case errors.Is(err, walletpin.ErrCurrentPINRequired),
		errors.Is(err, walletpin.ErrPINLocked):
		return status.Error(codes.FailedPrecondition, err.Error())

	case errors.Is(err, walletpin.ErrWrongPIN):
		return status.Error(codes.PermissionDenied, err.Error())

	default:
		h.logger.Error("wallet PIN request failed", "error", err)

		return status.Error(codes.Internal, "the request could not be completed")
	}
}

func toProtoPinStatus(s walletpin.Status) *identityv1.WalletPinStatusResponse {
	response := &identityv1.WalletPinStatusResponse{IsSet: s.IsSet, AttemptsLeft: int32(s.AttemptsLeft)}

	if s.SetAt != nil {
		response.SetAt = timestamppb.New(*s.SetAt)
	}

	if s.LockedUntil != nil {
		response.LockedUntil = timestamppb.New(s.LockedUntil.UTC().Truncate(time.Second))
	}

	return response
}

func toProtoPinCheck(check walletpin.Check) identityv1.PinCheck {
	switch check {
	case walletpin.CheckOK:
		return identityv1.PinCheck_PIN_CHECK_OK
	case walletpin.CheckWrong:
		return identityv1.PinCheck_PIN_CHECK_WRONG
	case walletpin.CheckLocked:
		return identityv1.PinCheck_PIN_CHECK_LOCKED
	case walletpin.CheckNotSet:
		return identityv1.PinCheck_PIN_CHECK_NOT_SET
	default:
		return identityv1.PinCheck_PIN_CHECK_UNSPECIFIED
	}
}

func looksLikeUUID(value string) bool {
	if len(value) != 36 {
		return false
	}

	for i, r := range value {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !('0' <= r && r <= '9' || 'a' <= r && r <= 'f' || 'A' <= r && r <= 'F') {
				return false
			}
		}
	}

	return true
}

package grpc

import (
	"context"
	"errors"

	walletv1 "github.com/7akoom/ride-platform/gen/go/ride/wallet/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/tips"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

// WithTips gives the handler tips.
func (h *WalletHandler) WithTips(service *tips.Service) *WalletHandler {
	h.tips = service

	return h
}

func (h *WalletHandler) TipDriver(
	ctx context.Context,
	request *walletv1.TipDriverRequest,
) (*walletv1.TipDriverResponse, error) {
	if h.tips == nil {
		return nil, status.Error(codes.Unimplemented, "tips are not available")
	}

	amount, err := parseMoney(request.GetAmount())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, tips.ErrInvalidAmount.Error())
	}

	given, balance, err := h.tips.Give(ctx, tips.Input{
		RiderID:        request.GetRiderId(),
		TripID:         request.GetTripId(),
		Amount:         amount,
		IdempotencyKey: request.GetIdempotencyKey(),
	})
	if err != nil {
		return nil, h.mapTipError(err)
	}

	// A retry returns the tip given the first time: the wallet as it is now.
	if balance.ID == "" {
		if balance, err = h.walletService.GetWallet(ctx, wallet.OwnerRider, request.GetRiderId()); err != nil {
			return nil, h.mapWalletError(err)
		}
	}

	return &walletv1.TipDriverResponse{
		Tip: &walletv1.Tip{
			Id:           given.ID,
			TripId:       given.TripID,
			Amount:       given.Amount.String(),
			CurrencyCode: given.CurrencyCode,
			CreatedAt:    timestamppb.New(given.CreatedAt),
		},
		Wallet: toProtoWallet(balance),
	}, nil
}

func (h *WalletHandler) mapTipError(err error) error {
	switch {
	case errors.Is(err, tips.ErrTripNotFound):
		return status.Error(codes.NotFound, err.Error())

	case errors.Is(err, tips.ErrKeyReused):
		return status.Error(codes.AlreadyExists, err.Error())

	case errors.Is(err, tips.ErrNotTippable),
		errors.Is(err, tips.ErrAlreadyTipped),
		errors.Is(err, tips.ErrBelowMin),
		errors.Is(err, tips.ErrAboveMax):
		return status.Error(codes.FailedPrecondition, err.Error())

	case errors.Is(err, tips.ErrTripRequired),
		errors.Is(err, tips.ErrInvalidAmount),
		errors.Is(err, tips.ErrIdempotencyKey):
		return status.Error(codes.InvalidArgument, err.Error())

	default:
		return h.mapWalletError(err)
	}
}

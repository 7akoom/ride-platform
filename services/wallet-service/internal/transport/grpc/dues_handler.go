package grpc

import (
	"context"
	"errors"

	walletv1 "github.com/7akoom/ride-platform/gen/go/ride/wallet/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/dues"
)

// WithDues gives staff the dues policy and the overdue riders.
func (h *WalletHandler) WithDues(service *dues.Service) *WalletHandler {
	h.dues = service

	return h
}

func (h *WalletHandler) GetDuesPolicy(ctx context.Context, _ *walletv1.GetDuesPolicyRequest) (*walletv1.DuesPolicy, error) {
	if h.dues == nil {
		return nil, status.Error(codes.Unimplemented, "dues are not available")
	}

	policy, err := h.dues.Policy(ctx)
	if err != nil {
		return nil, h.mapWalletError(err)
	}

	return &walletv1.DuesPolicy{BlockTripsWithDues: policy.BlockTrips, GraceDays: int32(policy.GraceDays)}, nil
}

func (h *WalletHandler) SetDuesPolicy(ctx context.Context, request *walletv1.DuesPolicy) (*walletv1.DuesPolicy, error) {
	if h.dues == nil {
		return nil, status.Error(codes.Unimplemented, "dues are not available")
	}

	policy, err := h.dues.SetPolicy(ctx, dues.Policy{
		BlockTrips: request.GetBlockTripsWithDues(),
		GraceDays:  int(request.GetGraceDays()),
	})
	if err != nil {
		if errors.Is(err, dues.ErrInvalidGraceDays) {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}

		return nil, h.mapWalletError(err)
	}

	return &walletv1.DuesPolicy{BlockTripsWithDues: policy.BlockTrips, GraceDays: int32(policy.GraceDays)}, nil
}

func (h *WalletHandler) ListOverdueRiders(
	ctx context.Context,
	request *walletv1.ListOverdueRidersRequest,
) (*walletv1.ListOverdueRidersResponse, error) {
	if h.dues == nil {
		return nil, status.Error(codes.Unimplemented, "dues are not available")
	}

	riders, err := h.dues.Overdue(ctx, int(request.GetLimit()))
	if err != nil {
		return nil, h.mapWalletError(err)
	}

	response := &walletv1.ListOverdueRidersResponse{Riders: make([]*walletv1.OverdueRider, 0, len(riders))}
	for _, rider := range riders {
		response.Riders = append(response.Riders, &walletv1.OverdueRider{
			RiderId:      rider.RiderID,
			CurrencyCode: rider.CurrencyCode,
			Outstanding:  rider.Outstanding.String(),
			OldestDueAt:  timestamppb.New(rider.OldestAt),
			PayBy:        timestamppb.New(rider.PayBy),
		})
	}

	return response, nil
}

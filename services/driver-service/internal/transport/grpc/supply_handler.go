package grpc

import (
	"context"

	driverv1 "github.com/7akoom/ride-platform/gen/go/ride/driver/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// SupplyFunc counts drivers by status, availability and class.
type SupplyFunc func(ctx context.Context) ([]*driverv1.DriverSupplyCount, error)

// WithSupply answers GetDriverSupply (analytics).
func (h *DriverHandler) WithSupply(count SupplyFunc) *DriverHandler {
	h.supply = count

	return h
}

// GetDriverSupply is internal only.
func (h *DriverHandler) GetDriverSupply(ctx context.Context, _ *driverv1.GetDriverSupplyRequest) (*driverv1.GetDriverSupplyResponse, error) {
	if h.supply == nil {
		return nil, status.Error(codes.Unimplemented, "driver supply is not available")
	}

	counts, err := h.supply(ctx)
	if err != nil {
		h.logger.Error("count drivers failed", "error", err)

		return nil, status.Error(codes.Internal, "failed to count drivers")
	}

	return &driverv1.GetDriverSupplyResponse{Counts: counts}, nil
}

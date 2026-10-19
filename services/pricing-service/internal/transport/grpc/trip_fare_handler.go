package grpc

import (
	"context"
	"errors"

	pricingv1 "github.com/7akoom/ride-platform/gen/go/ride/pricing/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/pricing"
)

// GetTripFare is the fare of one of the rider's trips, for the trip's details and
// its receipt in the Rider app.
func (h *PricingHandler) GetTripFare(
	ctx context.Context,
	request *pricingv1.GetTripFareRequest,
) (*pricingv1.GetTripFareResponse, error) {
	fare, err := h.pricingService.TripFare(ctx, request.GetRiderId(), request.GetTripId())
	if err != nil {
		if errors.Is(err, pricing.ErrFareNotFound) {
			return nil, status.Error(codes.NotFound, err.Error())
		}

		return nil, h.mapPricingError(err)
	}

	breakdown := toProtoFareBreakdown(fare.Breakdown)
	breakdown.Kind = string(fare.Kind)

	return &pricingv1.GetTripFareResponse{Fare: breakdown}, nil
}

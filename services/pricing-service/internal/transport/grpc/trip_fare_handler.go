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

// ListTripFares is the recorded totals of the trips named, for trip-service's lists.
func (h *PricingHandler) ListTripFares(
	ctx context.Context,
	request *pricingv1.ListTripFaresRequest,
) (*pricingv1.ListTripFaresResponse, error) {
	fares, err := h.pricingService.TripFares(ctx, request.GetTripIds())
	if err != nil {
		if errors.Is(err, pricing.ErrTooManyTrips) {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}

		return nil, h.mapPricingError(err)
	}

	response := &pricingv1.ListTripFaresResponse{Fares: make([]*pricingv1.TripFareTotal, 0, len(fares))}

	for _, fare := range fares {
		response.Fares = append(response.Fares, &pricingv1.TripFareTotal{
			TripId:       fare.TripID,
			Kind:         string(fare.Kind),
			Total:        fare.Breakdown.Total.String(),
			CurrencyCode: fare.Breakdown.CurrencyCode,
		})
	}

	return response, nil
}

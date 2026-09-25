package grpc

import (
	"context"

	pricingv1 "github.com/7akoom/ride-platform/gen/go/ride/pricing/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/pricing"
)

func (h *PricingHandler) QuoteTrip(
	ctx context.Context,
	request *pricingv1.QuoteTripRequest,
) (*pricingv1.QuoteTripResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	quotes, err := h.pricingService.QuoteTrip(ctx, pricing.QuoteTripInput{
		RiderID:    request.GetRiderId(),
		PickupLat:  request.GetPickup().GetLatitude(),
		PickupLng:  request.GetPickup().GetLongitude(),
		DropoffLat: request.GetDropoff().GetLatitude(),
		DropoffLng: request.GetDropoff().GetLongitude(),
		CouponCode: request.GetCouponCode(),
		Stops:      toDomainPoints(request.GetStops()),
	})
	if err != nil {
		return nil, h.mapPricingError(err)
	}

	response := &pricingv1.QuoteTripResponse{ZoneId: quotes.ZoneID, CityId: quotes.CityID}

	for _, quote := range quotes.Quotes {
		response.Quotes = append(response.Quotes, &pricingv1.TripQuote{
			QuoteId:          quote.ID,
			VehicleClass:     quote.VehicleClass,
			Fare:             toProtoFareBreakdown(quote.Breakdown),
			ExpiresAt:        timestamppb.New(quote.ExpiresAt),
			DriversAvailable: quote.DriversAvailable,
			PickupEtaMinutes: int32(quote.PickupETAMinutes),
		})
	}

	return response, nil
}

func (h *PricingHandler) ClaimQuote(
	ctx context.Context,
	request *pricingv1.ClaimQuoteRequest,
) (*pricingv1.ClaimQuoteResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	quote, err := h.pricingService.ClaimQuote(ctx, request.GetQuoteId(), request.GetRiderId(), request.GetTripId())
	if err != nil {
		return nil, h.mapPricingError(err)
	}

	return &pricingv1.ClaimQuoteResponse{
		QuoteId:      quote.ID,
		VehicleClass: quote.VehicleClass,
		Pickup:       &pricingv1.Coordinates{Latitude: quote.Pickup.Latitude, Longitude: quote.Pickup.Longitude},
		Dropoff:      &pricingv1.Coordinates{Latitude: quote.Dropoff.Latitude, Longitude: quote.Dropoff.Longitude},
		Total:        quote.Breakdown.Total.String(),
		CurrencyCode: quote.Breakdown.CurrencyCode,
		ZoneId:       quote.ZoneID,
		Stops:        toProtoPoints(quote.Stops),
	}, nil
}

func (h *PricingHandler) ReleaseQuote(
	ctx context.Context,
	request *pricingv1.ReleaseQuoteRequest,
) (*pricingv1.ReleaseQuoteResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	if err := h.pricingService.ReleaseQuote(ctx, request.GetQuoteId(), request.GetTripId()); err != nil {
		return nil, h.mapPricingError(err)
	}

	return &pricingv1.ReleaseQuoteResponse{}, nil
}

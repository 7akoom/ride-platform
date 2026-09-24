package grpc

import (
	"context"
	"errors"
	"log/slog"

	pricingv1 "github.com/7akoom/ride-platform/gen/go/ride/pricing/v1"
	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/pricing"
	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/promotions"
	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/tariffs"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type PricingHandler struct {
	pricingv1.UnimplementedPricingServiceServer

	pricingService pricing.Service
	tariffs        *tariffs.Service
	promotions     *promotions.Service
	logger         *slog.Logger
}

func NewPricingHandler(
	pricingService pricing.Service,
	tariffService *tariffs.Service,
	promotionService *promotions.Service,
	logger *slog.Logger,
) *PricingHandler {
	if pricingService == nil {
		panic("pricing service is required")
	}

	if tariffService == nil {
		panic("tariff service is required")
	}

	if promotionService == nil {
		panic("promotion service is required")
	}

	if logger == nil {
		panic("logger is required")
	}

	return &PricingHandler{pricingService: pricingService, tariffs: tariffService, promotions: promotionService, logger: logger}
}

func (h *PricingHandler) EstimateFare(
	ctx context.Context,
	request *pricingv1.EstimateFareRequest,
) (*pricingv1.EstimateFareResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	pickup := request.GetPickup()
	dropoff := request.GetDropoff()

	breakdown, err := h.pricingService.EstimateFare(ctx, pricing.EstimateFareInput{
		RiderID:      request.GetRiderId(),
		PickupLat:    pickup.GetLatitude(),
		PickupLng:    pickup.GetLongitude(),
		DropoffLat:   dropoff.GetLatitude(),
		DropoffLng:   dropoff.GetLongitude(),
		CouponCode:   request.GetCouponCode(),
		VehicleClass: request.GetVehicleClass(),
	})
	if err != nil {
		return nil, h.mapPricingError(err)
	}

	return &pricingv1.EstimateFareResponse{
		Fare: toProtoFareBreakdown(breakdown),
	}, nil
}

func (h *PricingHandler) CalculateFare(
	ctx context.Context,
	request *pricingv1.CalculateFareRequest,
) (*pricingv1.CalculateFareResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	pickup := request.GetPickup()
	dropoff := request.GetDropoff()

	fare, err := h.pricingService.CalculateFare(ctx, pricing.CalculateFareInput{
		TripID:       request.GetTripId(),
		RiderID:      request.GetRiderId(),
		PickupLat:    pickup.GetLatitude(),
		PickupLng:    pickup.GetLongitude(),
		DropoffLat:   dropoff.GetLatitude(),
		DropoffLng:   dropoff.GetLongitude(),
		CouponCode:   request.GetCouponCode(),
		VehicleClass: request.GetVehicleClass(),
		QuoteID:      request.GetQuoteId(),
	})
	if err != nil {
		return nil, h.mapPricingError(err)
	}

	breakdown := toProtoFareBreakdown(fare.Breakdown)
	breakdown.Kind = string(fare.Kind)

	return &pricingv1.CalculateFareResponse{Fare: breakdown}, nil
}

// mapPricingError classifies known domain errors into the right gRPC
// status. Anything unclassified is logged here, server-side, with its
// real message — then mapped to a generic Internal status for the
// client. The client never sees err.Error() for an unclassified
// failure; the logger is what makes that failure diagnosable instead
// of silent.
func (h *PricingHandler) mapPricingError(err error) error {
	switch {
	case errors.Is(err, pricing.ErrNoActiveConfig):
		return status.Error(codes.FailedPrecondition, err.Error())

	case errors.Is(err, pricing.ErrPickupOutsideServiceZone):
		return status.Error(codes.InvalidArgument, err.Error())

	case errors.Is(err, pricing.ErrQuoteNotFound):
		return status.Error(codes.NotFound, err.Error())

	case errors.Is(err, pricing.ErrQuoteExpired),
		errors.Is(err, pricing.ErrQuoteAlreadyUsed),
		errors.Is(err, pricing.ErrQuoteCouponUnavailable),
		errors.Is(err, pricing.ErrQuoteNotForTrip):
		return status.Error(codes.FailedPrecondition, err.Error())

	case errors.Is(err, pricing.ErrRiderIDRequired),
		errors.Is(err, pricing.ErrTripIDRequired),
		errors.Is(err, pricing.ErrInvalidLatitude),
		errors.Is(err, pricing.ErrInvalidLongitude),
		errors.Is(err, pricing.ErrInvalidVehicleClass),
		errors.Is(err, pricing.ErrQuoteIDRequired):
		return status.Error(codes.InvalidArgument, err.Error())

	case upstreamUnavailable(err):
		h.logger.Warn("a service pricing depends on is unavailable", "error", err)

		return status.Error(codes.Unavailable, "pricing is unavailable right now; try again")

	default:
		h.logger.Error("unclassified pricing request failure", "error", err)

		return status.Error(codes.Internal, "failed to process pricing request")
	}
}

// upstreamUnavailable reports an error that came from another service
// (location, driver, staff) being down or slow: the caller may retry.
func upstreamUnavailable(err error) bool {
	switch status.Code(err) {
	case codes.Unavailable, codes.DeadlineExceeded:
		return true
	default:
		return false
	}
}

func toDomainDiscountType(t pricingv1.DiscountType) pricing.DiscountType {
	switch t {
	case pricingv1.DiscountType_DISCOUNT_TYPE_PERCENTAGE:
		return pricing.DiscountPercentage
	case pricingv1.DiscountType_DISCOUNT_TYPE_FIXED_AMOUNT:
		return pricing.DiscountFixed
	default:
		return pricing.DiscountNone
	}
}

func toProtoDiscountType(t pricing.DiscountType) pricingv1.DiscountType {
	switch t {
	case pricing.DiscountPercentage:
		return pricingv1.DiscountType_DISCOUNT_TYPE_PERCENTAGE
	case pricing.DiscountFixed:
		return pricingv1.DiscountType_DISCOUNT_TYPE_FIXED_AMOUNT
	default:
		return pricingv1.DiscountType_DISCOUNT_TYPE_UNSPECIFIED
	}
}

func toProtoFareBreakdown(b pricing.FareBreakdown) *pricingv1.FareBreakdown {
	return &pricingv1.FareBreakdown{
		CurrencyCode:          b.CurrencyCode,
		ZoneId:                b.ZoneID,
		CityId:                b.CityID,
		VehicleClass:          b.VehicleClass,
		BaseFare:              b.BaseFare.String(),
		DistanceKm:            b.DistanceKm,
		DistanceFare:          b.DistanceFare.String(),
		DurationMinutes:       b.DurationMinutes,
		DurationFare:          b.DurationFare.String(),
		MinimumFareAdjustment: b.MinimumFareAdjustment.String(),
		Subtotal:              b.Subtotal.String(),
		WaitingMinutes:        int32(b.WaitingMinutes),
		WaitingFare:           b.WaitingFare.String(),
		Surge: &pricingv1.SurgeBreakdown{
			TimeOfDayPercent: b.Surge.TimeOfDayPercent.String(),
			ZonePercent:      b.Surge.ZonePercent.String(),
			DemandPercent:    b.Surge.DemandPercent.String(),
			WeatherPercent:   b.Surge.WeatherPercent.String(),
			TotalPercent:     b.Surge.TotalPercent.String(),
			Multiplier:       b.Surge.Multiplier.String(),
			Label:            b.Surge.Label,
		},
		SurgeAmount:          b.SurgeAmount.String(),
		AppliedDiscountType:  toProtoDiscountType(b.AppliedDiscountType),
		AppliedDiscountLabel: b.AppliedDiscountLabel,
		DiscountAmount:       b.DiscountAmount.String(),
		Total:                b.Total.String(),
		CouponStatus:         toProtoCouponStatus(b.CouponStatus),
	}
}

package grpc

import (
	"context"
	"errors"
	"log/slog"

	pricingv1 "github.com/7akoom/ride-platform/gen/go/ride/pricing/v1"
	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/pricing"
	"github.com/shopspring/decimal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type PricingHandler struct {
	pricingv1.UnimplementedPricingServiceServer

	pricingService pricing.Service
	logger         *slog.Logger
}

func NewPricingHandler(
	pricingService pricing.Service,
	logger *slog.Logger,
) *PricingHandler {
	if pricingService == nil {
		panic("pricing service is required")
	}

	if logger == nil {
		panic("logger is required")
	}

	return &PricingHandler{pricingService: pricingService, logger: logger}
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
	})
	if err != nil {
		return nil, h.mapPricingError(err)
	}

	return &pricingv1.CalculateFareResponse{
		Fare: toProtoFareBreakdown(fare.Breakdown),
	}, nil
}

func (h *PricingHandler) CreateCoupon(
	ctx context.Context,
	request *pricingv1.CreateCouponRequest,
) (*pricingv1.CreateCouponResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	discountValue, err := parseMoney(request.GetDiscountValue())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "discount_value must be a valid decimal value")
	}

	minimumFareAmount, err := parseMoney(request.GetMinimumFareAmount())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "minimum_fare_amount must be a valid decimal value")
	}

	var maxRedemptions *int

	if request.GetMaxRedemptions() > 0 {
		value := int(request.GetMaxRedemptions())
		maxRedemptions = &value
	}

	coupon, err := h.pricingService.CreateCoupon(ctx, pricing.CreateCouponInput{
		Code:              request.GetCode(),
		DiscountType:      toDomainDiscountType(request.GetDiscountType()),
		DiscountValue:     discountValue,
		ValidFrom:         request.GetValidFrom().AsTime(),
		ValidUntil:        request.GetValidUntil().AsTime(),
		MaxRedemptions:    maxRedemptions,
		PerRiderLimit:     int(request.GetPerRiderLimit()),
		MinimumFareAmount: minimumFareAmount,
	})
	if err != nil {
		return nil, h.mapPricingError(err)
	}

	return &pricingv1.CreateCouponResponse{
		Coupon: toProtoCoupon(coupon),
	}, nil
}

func (h *PricingHandler) GetCoupon(
	ctx context.Context,
	request *pricingv1.GetCouponRequest,
) (*pricingv1.GetCouponResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	coupon, err := h.pricingService.GetCoupon(ctx, request.GetCode())
	if err != nil {
		return nil, h.mapPricingError(err)
	}

	return &pricingv1.GetCouponResponse{
		Coupon: toProtoCoupon(coupon),
	}, nil
}

// mapPricingError classifies known domain errors into the right gRPC
// status. Anything unclassified is logged here, server-side, with its
// real message — then mapped to a generic Internal status for the
// client. The client never sees err.Error() for an unclassified
// failure; the logger is what makes that failure diagnosable instead
// of silent.
func (h *PricingHandler) mapPricingError(err error) error {
	switch {
	case errors.Is(err, pricing.ErrCouponNotFound):
		return status.Error(codes.NotFound, "coupon not found")

	case errors.Is(err, pricing.ErrCouponAlreadyExists):
		return status.Error(codes.AlreadyExists, "a coupon with this code already exists")

	case errors.Is(err, pricing.ErrNoActiveConfig):
		return status.Error(codes.FailedPrecondition, err.Error())

	case errors.Is(err, pricing.ErrPickupOutsideServiceZone):
		return status.Error(codes.InvalidArgument, err.Error())

	case errors.Is(err, pricing.ErrRiderIDRequired),
		errors.Is(err, pricing.ErrTripIDRequired),
		errors.Is(err, pricing.ErrInvalidLatitude),
		errors.Is(err, pricing.ErrInvalidLongitude),
		errors.Is(err, pricing.ErrCouponCodeRequired),
		errors.Is(err, pricing.ErrInvalidDiscountType),
		errors.Is(err, pricing.ErrInvalidDiscountValue),
		errors.Is(err, pricing.ErrInvalidValidityWindow),
		errors.Is(err, pricing.ErrInvalidVehicleClass):
		return status.Error(codes.InvalidArgument, err.Error())

	default:
		h.logger.Error("unclassified pricing request failure", "error", err)

		return status.Error(codes.Internal, "failed to process pricing request")
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

// parseMoney converts the wire's decimal string into exact decimal.
// Money crosses the wire as a string, never a float — a float64/double
// field in protobuf would reintroduce exactly the precision problem
// the NUMERIC columns exist to avoid (same helper as wallet-service's
// own transport layer).
func parseMoney(value string) (decimal.Decimal, error) {
	if value == "" {
		return decimal.Zero, nil
	}

	return decimal.NewFromString(value)
}

func toProtoFareBreakdown(b pricing.FareBreakdown) *pricingv1.FareBreakdown {
	return &pricingv1.FareBreakdown{
		CurrencyCode:    b.CurrencyCode,
		ZoneId:          b.ZoneID,
		VehicleClass:    b.VehicleClass,
		BaseFare:        b.BaseFare.String(),
		DistanceKm:      b.DistanceKm,
		DistanceFare:    b.DistanceFare.String(),
		DurationMinutes: b.DurationMinutes,
		DurationFare:    b.DurationFare.String(),
		Subtotal:        b.Subtotal.String(),
		Surge: &pricingv1.SurgeBreakdown{
			TimeOfDayPercent: b.Surge.TimeOfDayPercent.String(),
			DemandPercent:    b.Surge.DemandPercent.String(),
			WeatherPercent:   b.Surge.WeatherPercent.String(),
			TotalPercent:     b.Surge.TotalPercent.String(),
			Multiplier:       b.Surge.Multiplier.String(),
		},
		SurgeAmount:          b.SurgeAmount.String(),
		AppliedDiscountType:  toProtoDiscountType(b.AppliedDiscountType),
		AppliedDiscountLabel: b.AppliedDiscountLabel,
		DiscountAmount:       b.DiscountAmount.String(),
		Total:                b.Total.String(),
	}
}

func toProtoCoupon(c pricing.Coupon) *pricingv1.Coupon {
	maxRedemptions := int32(0)

	if c.MaxRedemptions != nil {
		maxRedemptions = int32(*c.MaxRedemptions)
	}

	return &pricingv1.Coupon{
		Code:              c.Code,
		DiscountType:      toProtoDiscountType(c.DiscountType),
		DiscountValue:     c.DiscountValue.String(),
		ValidFrom:         timestamppb.New(c.ValidFrom),
		ValidUntil:        timestamppb.New(c.ValidUntil),
		MaxRedemptions:    maxRedemptions,
		RedemptionCount:   int32(c.RedemptionCount),
		PerRiderLimit:     int32(c.PerRiderLimit),
		MinimumFareAmount: c.MinimumFareAmount.String(),
		Active:            c.Active,
	}
}

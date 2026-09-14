package grpc

import (
	"context"
	"errors"

	pricingv1 "github.com/7akoom/ride-platform/gen/go/ride/pricing/v1"
	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/pricing"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type PricingHandler struct {
	pricingv1.UnimplementedPricingServiceServer

	pricingService pricing.Service
}

func NewPricingHandler(
	pricingService pricing.Service,
) *PricingHandler {
	if pricingService == nil {
		panic("pricing service is required")
	}

	return &PricingHandler{pricingService: pricingService}
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
		RiderID:    request.GetRiderId(),
		PickupLat:  pickup.GetLatitude(),
		PickupLng:  pickup.GetLongitude(),
		DropoffLat: dropoff.GetLatitude(),
		DropoffLng: dropoff.GetLongitude(),
		CouponCode: request.GetCouponCode(),
	})
	if err != nil {
		return nil, mapPricingError(err)
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
		TripID:     request.GetTripId(),
		RiderID:    request.GetRiderId(),
		PickupLat:  pickup.GetLatitude(),
		PickupLng:  pickup.GetLongitude(),
		DropoffLat: dropoff.GetLatitude(),
		DropoffLng: dropoff.GetLongitude(),
		CouponCode: request.GetCouponCode(),
	})
	if err != nil {
		return nil, mapPricingError(err)
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

	var maxRedemptions *int

	if request.GetMaxRedemptions() > 0 {
		value := int(request.GetMaxRedemptions())
		maxRedemptions = &value
	}

	coupon, err := h.pricingService.CreateCoupon(ctx, pricing.CreateCouponInput{
		Code:              request.GetCode(),
		DiscountType:      toDomainDiscountType(request.GetDiscountType()),
		DiscountValue:     request.GetDiscountValue(),
		ValidFrom:         request.GetValidFrom().AsTime(),
		ValidUntil:        request.GetValidUntil().AsTime(),
		MaxRedemptions:    maxRedemptions,
		PerRiderLimit:     int(request.GetPerRiderLimit()),
		MinimumFareAmount: request.GetMinimumFareAmount(),
	})
	if err != nil {
		return nil, mapPricingError(err)
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
		return nil, mapPricingError(err)
	}

	return &pricingv1.GetCouponResponse{
		Coupon: toProtoCoupon(coupon),
	}, nil
}

func mapPricingError(err error) error {
	switch {
	case errors.Is(err, pricing.ErrCouponNotFound):
		return status.Error(codes.NotFound, "coupon not found")

	case errors.Is(err, pricing.ErrCouponAlreadyExists):
		return status.Error(codes.AlreadyExists, "a coupon with this code already exists")

	case errors.Is(err, pricing.ErrNoActiveConfig):
		return status.Error(codes.FailedPrecondition, err.Error())

	case errors.Is(err, pricing.ErrRiderIDRequired),
		errors.Is(err, pricing.ErrTripIDRequired),
		errors.Is(err, pricing.ErrInvalidLatitude),
		errors.Is(err, pricing.ErrInvalidLongitude),
		errors.Is(err, pricing.ErrCouponCodeRequired),
		errors.Is(err, pricing.ErrInvalidDiscountType),
		errors.Is(err, pricing.ErrInvalidDiscountValue),
		errors.Is(err, pricing.ErrInvalidValidityWindow):
		return status.Error(codes.InvalidArgument, err.Error())

	default:
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

func toProtoFareBreakdown(b pricing.FareBreakdown) *pricingv1.FareBreakdown {
	return &pricingv1.FareBreakdown{
		CurrencyCode:    b.CurrencyCode,
		BaseFare:        b.BaseFare,
		DistanceKm:      b.DistanceKm,
		DistanceFare:    b.DistanceFare,
		DurationMinutes: b.DurationMinutes,
		DurationFare:    b.DurationFare,
		Subtotal:        b.Subtotal,
		Surge: &pricingv1.SurgeBreakdown{
			TimeOfDayPercent: b.Surge.TimeOfDayPercent,
			DemandPercent:    b.Surge.DemandPercent,
			WeatherPercent:   b.Surge.WeatherPercent,
			TotalPercent:     b.Surge.TotalPercent,
			Multiplier:       b.Surge.Multiplier,
		},
		SurgeAmount:          b.SurgeAmount,
		AppliedDiscountType:  toProtoDiscountType(b.AppliedDiscountType),
		AppliedDiscountLabel: b.AppliedDiscountLabel,
		DiscountAmount:       b.DiscountAmount,
		Total:                b.Total,
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
		DiscountValue:     c.DiscountValue,
		ValidFrom:         timestamppb.New(c.ValidFrom),
		ValidUntil:        timestamppb.New(c.ValidUntil),
		MaxRedemptions:    maxRedemptions,
		RedemptionCount:   int32(c.RedemptionCount),
		PerRiderLimit:     int32(c.PerRiderLimit),
		MinimumFareAmount: c.MinimumFareAmount,
		Active:            c.Active,
	}
}

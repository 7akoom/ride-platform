package grpc

import (
	"context"
	"errors"

	pricingv1 "github.com/7akoom/ride-platform/gen/go/ride/pricing/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/promotions"
)

// --- a rider's offers ------------------------------------------------------------

func (h *PricingHandler) ListRiderOffers(
	ctx context.Context,
	request *pricingv1.ListRiderOffersRequest,
) (*pricingv1.ListRiderOffersResponse, error) {
	offers, err := h.promotions.RiderOffers(ctx, request.GetRiderId())
	if err != nil {
		return nil, h.mapOfferError(err)
	}

	response := &pricingv1.ListRiderOffersResponse{Coupons: []*pricingv1.CouponOffer{}}

	for _, offer := range offers.Coupons {
		response.Coupons = append(response.Coupons, toProtoCouponOffer(offer))
	}

	if first := offers.FirstRide; first != nil {
		response.FirstRide = &pricingv1.AutomaticOffer{
			Percent:   first.Percent.String(),
			MaxAmount: decimalString(first.MaxAmount),
		}
	}

	if loyalty := offers.Loyalty; loyalty != nil {
		response.Loyalty = &pricingv1.LoyaltyOffer{
			Every:          int32(loyalty.Every),
			Percent:        loyalty.Percent.String(),
			MaxAmount:      decimalString(loyalty.MaxAmount),
			CompletedTrips: int32(loyalty.CompletedTrips),
			TripsToGo:      int32(loyalty.TripsToGo),
		}
	}

	return response, nil
}

func (h *PricingHandler) CheckRiderCoupon(
	ctx context.Context,
	request *pricingv1.CheckRiderCouponRequest,
) (*pricingv1.CheckRiderCouponResponse, error) {
	check, err := h.promotions.CheckCoupon(ctx, request.GetRiderId(), request.GetCode())
	if err != nil {
		return nil, h.mapOfferError(err)
	}

	response := &pricingv1.CheckRiderCouponResponse{Status: toProtoCouponStatus(check.Status)}

	if check.Offer != nil {
		response.Coupon = toProtoCouponOffer(*check.Offer)
	}

	return response, nil
}

func (h *PricingHandler) mapOfferError(err error) error {
	if errors.Is(err, promotions.ErrRiderRequired) {
		return status.Error(codes.InvalidArgument, err.Error())
	}

	h.logger.Error("rider offers failed", "error", err)

	return status.Error(codes.Internal, "failed to read the offers")
}

// toProtoCouponOffer is a coupon as a rider sees it: never its uses, who
// made it, or where exactly it applies.
func toProtoCouponOffer(offer promotions.CouponOffer) *pricingv1.CouponOffer {
	c := offer.Coupon

	return &pricingv1.CouponOffer{
		Code:              c.Code,
		Description:       c.Description,
		DiscountType:      toProtoDiscountType(c.DiscountType),
		DiscountValue:     c.DiscountValue.String(),
		MaxDiscountAmount: decimalString(c.MaxDiscountAmount),
		MinimumFareAmount: c.MinimumFareAmount.String(),
		ValidUntil:        timestamppb.New(c.ValidUntil),
		VehicleClasses:    c.VehicleClasses,
		LimitedArea:       c.CityID != "" || c.ZoneID != "",
		UsesLeft:          int32(offer.UsesLeft),
		NewRidersOnly:     c.NewRidersOnly,
	}
}

package grpc

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	pricingv1 "github.com/7akoom/ride-platform/gen/go/ride/pricing/v1"
	"github.com/shopspring/decimal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/pricing"
	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/promotions"
)

// --- coupons -----------------------------------------------------------------

func (h *PricingHandler) ListCoupons(
	ctx context.Context,
	request *pricingv1.ListCouponsRequest,
) (*pricingv1.ListCouponsResponse, error) {
	page, err := h.promotions.ListCoupons(ctx, request.GetState(), request.GetQuery(), int(request.GetPageSize()), request.GetPageToken())
	if err != nil {
		return nil, h.mapPromotionError(err)
	}

	now := time.Now().UTC()
	response := &pricingv1.ListCouponsResponse{NextPageToken: nextToken(page.NextOffset)}

	for _, coupon := range page.Items {
		response.Coupons = append(response.Coupons, toProtoCoupon(coupon, now))
	}

	return response, nil
}

func (h *PricingHandler) CreateCoupon(
	ctx context.Context,
	request *pricingv1.CreateCouponRequest,
) (*pricingv1.CreateCouponResponse, error) {
	input := promotions.CouponInput{
		Code:           request.GetCode(),
		Description:    request.GetDescription(),
		DiscountType:   toDomainDiscountType(request.GetDiscountType()),
		MaxRedemptions: int(request.GetMaxRedemptions()),
		PerRiderLimit:  int(request.GetPerRiderLimit()),
		CityID:         request.GetCityId(),
		ZoneID:         request.GetZoneId(),
		VehicleClasses: request.GetVehicleClasses(),
		NewRidersOnly:  request.GetNewRidersOnly(),
	}

	if request.GetValidFrom() != nil {
		input.ValidFrom = request.GetValidFrom().AsTime()
	}

	if request.GetValidUntil() != nil {
		input.ValidUntil = request.GetValidUntil().AsTime()
	}

	var err error

	if input.DiscountValue, err = decimalField("discount_value", request.GetDiscountValue(), true); err != nil {
		return nil, err
	}

	if input.MinimumFareAmount, err = decimalField("minimum_fare_amount", request.GetMinimumFareAmount(), false); err != nil {
		return nil, err
	}

	if input.MaxDiscountAmount, err = optionalDecimal("max_discount_amount", request.GetMaxDiscountAmount()); err != nil {
		return nil, err
	}

	coupon, err := h.promotions.CreateCoupon(ctx, input, staffIdentity(ctx))
	if err != nil {
		return nil, h.mapPromotionError(err)
	}

	return &pricingv1.CreateCouponResponse{Coupon: toProtoCoupon(coupon, time.Now().UTC())}, nil
}

func (h *PricingHandler) GetCoupon(
	ctx context.Context,
	request *pricingv1.GetCouponRequest,
) (*pricingv1.GetCouponResponse, error) {
	details, err := h.promotions.GetCoupon(ctx, request.GetCode())
	if err != nil {
		return nil, h.mapPromotionError(err)
	}

	coupon := toProtoCoupon(details.Coupon, time.Now().UTC())
	coupon.RedeemedCount = int32(details.RedeemedCount)
	coupon.DiscountGiven = details.DiscountGiven.String()

	return &pricingv1.GetCouponResponse{Coupon: coupon}, nil
}

func (h *PricingHandler) UpdateCoupon(
	ctx context.Context,
	request *pricingv1.UpdateCouponRequest,
) (*pricingv1.CreateCouponResponse, error) {
	change := promotions.CouponChange{
		Description: request.Description,
		Active:      request.Active,
	}

	if request.GetValidUntil() != nil {
		until := request.GetValidUntil().AsTime()
		change.ValidUntil = &until
	}

	if request.MaxRedemptions != nil {
		value := int(request.GetMaxRedemptions())
		change.MaxRedemptions = &value
	}

	if request.PerRiderLimit != nil {
		value := int(request.GetPerRiderLimit())
		change.PerRiderLimit = &value
	}

	if request.MinimumFareAmount != nil {
		amount, err := decimalField("minimum_fare_amount", request.GetMinimumFareAmount(), true)
		if err != nil {
			return nil, err
		}

		change.MinimumFareAmount = &amount
	}

	coupon, err := h.promotions.UpdateCoupon(ctx, request.GetCode(), change, staffIdentity(ctx))
	if err != nil {
		return nil, h.mapPromotionError(err)
	}

	return &pricingv1.CreateCouponResponse{Coupon: toProtoCoupon(coupon, time.Now().UTC())}, nil
}

func (h *PricingHandler) ListCouponRedemptions(
	ctx context.Context,
	request *pricingv1.ListCouponRedemptionsRequest,
) (*pricingv1.ListCouponRedemptionsResponse, error) {
	page, err := h.promotions.ListRedemptions(ctx, request.GetCode(), int(request.GetPageSize()), request.GetPageToken())
	if err != nil {
		return nil, h.mapPromotionError(err)
	}

	response := &pricingv1.ListCouponRedemptionsResponse{NextPageToken: nextToken(page.NextOffset)}

	for _, redemption := range page.Items {
		entry := &pricingv1.CouponRedemption{
			Id:             redemption.ID,
			RiderId:        redemption.RiderID,
			TripId:         redemption.TripID,
			QuoteId:        redemption.QuoteID,
			Status:         redemption.Status,
			DiscountAmount: redemption.DiscountAmount.String(),
			CreatedAt:      timestamppb.New(redemption.CreatedAt),
		}

		if redemption.ReleasedAt != nil {
			entry.ReleasedAt = timestamppb.New(*redemption.ReleasedAt)
		}

		response.Redemptions = append(response.Redemptions, entry)
	}

	return response, nil
}

// --- automatic discounts ---------------------------------------------------------

func (h *PricingHandler) GetPromotionSettings(
	ctx context.Context,
	_ *pricingv1.GetPromotionSettingsRequest,
) (*pricingv1.PromotionSettingsResponse, error) {
	settings, err := h.promotions.GetSettings(ctx)
	if err != nil {
		return nil, h.mapPromotionError(err)
	}

	return &pricingv1.PromotionSettingsResponse{Settings: toProtoPromotionSettings(settings)}, nil
}

func (h *PricingHandler) UpdatePromotionSettings(
	ctx context.Context,
	request *pricingv1.UpdatePromotionSettingsRequest,
) (*pricingv1.PromotionSettingsResponse, error) {
	input := promotions.SettingsInput{LoyaltyEvery: int(request.GetLoyaltyEvery())}

	var err error

	if input.FirstRidePercent, err = decimalField("first_ride_percent", request.GetFirstRidePercent(), true); err != nil {
		return nil, err
	}

	if input.LoyaltyPercent, err = decimalField("loyalty_percent", request.GetLoyaltyPercent(), true); err != nil {
		return nil, err
	}

	if input.FirstRideMaxAmount, err = optionalDecimal("first_ride_max_amount", request.GetFirstRideMaxAmount()); err != nil {
		return nil, err
	}

	if input.LoyaltyMaxAmount, err = optionalDecimal("loyalty_max_amount", request.GetLoyaltyMaxAmount()); err != nil {
		return nil, err
	}

	settings, err := h.promotions.UpdateSettings(ctx, input, staffIdentity(ctx))
	if err != nil {
		return nil, h.mapPromotionError(err)
	}

	return &pricingv1.PromotionSettingsResponse{Settings: toProtoPromotionSettings(settings)}, nil
}

func (h *PricingHandler) mapPromotionError(err error) error {
	var invalidErr *promotions.InvalidError

	switch {
	case errors.As(err, &invalidErr):
		return status.Error(codes.InvalidArgument, invalidErr.Error())

	case errors.Is(err, pricing.ErrCouponNotFound),
		errors.Is(err, promotions.ErrCityNotFound),
		errors.Is(err, promotions.ErrZoneNotFound):
		return status.Error(codes.NotFound, err.Error())

	case errors.Is(err, pricing.ErrCouponAlreadyExists):
		return status.Error(codes.AlreadyExists, err.Error())

	case errors.Is(err, promotions.ErrPlacesUnavailable):
		h.logger.Warn("cities and zones could not be checked", "error", err)

		return status.Error(codes.Unavailable, promotions.ErrPlacesUnavailable.Error())

	default:
		h.logger.Error("unclassified promotion request failure", "error", err)

		return status.Error(codes.Internal, "failed to process pricing request")
	}
}

// optionalDecimal is nil for an empty value.
func optionalDecimal(name, value string) (*decimal.Decimal, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}

	amount, err := decimalField(name, value, true)
	if err != nil {
		return nil, err
	}

	return &amount, nil
}

func decimalString(amount *decimal.Decimal) string {
	if amount == nil {
		return ""
	}

	return amount.String()
}

func nextToken(offset int) string {
	if offset <= 0 {
		return ""
	}

	return strconv.Itoa(offset)
}

func toProtoCoupon(c pricing.Coupon, now time.Time) *pricingv1.Coupon {
	maxRedemptions := int32(0)
	if c.MaxRedemptions != nil {
		maxRedemptions = int32(*c.MaxRedemptions)
	}

	coupon := &pricingv1.Coupon{
		Id:                c.ID,
		Code:              c.Code,
		Description:       c.Description,
		DiscountType:      toProtoDiscountType(c.DiscountType),
		DiscountValue:     c.DiscountValue.String(),
		MaxDiscountAmount: decimalString(c.MaxDiscountAmount),
		ValidFrom:         timestamppb.New(c.ValidFrom),
		ValidUntil:        timestamppb.New(c.ValidUntil),
		MaxRedemptions:    maxRedemptions,
		RedemptionCount:   int32(c.RedemptionCount),
		PerRiderLimit:     int32(c.PerRiderLimit),
		MinimumFareAmount: c.MinimumFareAmount.String(),
		Active:            c.Active,
		CityId:            c.CityID,
		ZoneId:            c.ZoneID,
		VehicleClasses:    c.VehicleClasses,
		NewRidersOnly:     c.NewRidersOnly,
		CreatedBy:         c.CreatedBy,
		UpdatedBy:         c.UpdatedBy,
		State:             c.State(now),
	}

	if !c.CreatedAt.IsZero() {
		coupon.CreatedAt = timestamppb.New(c.CreatedAt)
	}

	if !c.UpdatedAt.IsZero() {
		coupon.UpdatedAt = timestamppb.New(c.UpdatedAt)
	}

	return coupon
}

func toProtoPromotionSettings(s pricing.PromotionSettings) *pricingv1.PromotionSettings {
	settings := &pricingv1.PromotionSettings{
		FirstRidePercent:   s.FirstRidePercent.String(),
		FirstRideMaxAmount: decimalString(s.FirstRideMaxAmount),
		LoyaltyEvery:       int32(s.LoyaltyEvery),
		LoyaltyPercent:     s.LoyaltyPercent.String(),
		LoyaltyMaxAmount:   decimalString(s.LoyaltyMaxAmount),
		UpdatedBy:          s.UpdatedBy,
	}

	if !s.UpdatedAt.IsZero() {
		settings.UpdatedAt = timestamppb.New(s.UpdatedAt)
	}

	return settings
}

var couponStatuses = map[pricing.CouponStatus]pricingv1.CouponStatus{
	pricing.CouponStatusApplied:        pricingv1.CouponStatus_COUPON_STATUS_APPLIED,
	pricing.CouponStatusNotFound:       pricingv1.CouponStatus_COUPON_STATUS_NOT_FOUND,
	pricing.CouponStatusEnded:          pricingv1.CouponStatus_COUPON_STATUS_ENDED,
	pricing.CouponStatusNotStarted:     pricingv1.CouponStatus_COUPON_STATUS_NOT_STARTED,
	pricing.CouponStatusExpired:        pricingv1.CouponStatus_COUPON_STATUS_EXPIRED,
	pricing.CouponStatusUsedUp:         pricingv1.CouponStatus_COUPON_STATUS_USED_UP,
	pricing.CouponStatusAlreadyUsed:    pricingv1.CouponStatus_COUPON_STATUS_ALREADY_USED,
	pricing.CouponStatusNotInArea:      pricingv1.CouponStatus_COUPON_STATUS_NOT_IN_AREA,
	pricing.CouponStatusNotForClass:    pricingv1.CouponStatus_COUPON_STATUS_NOT_FOR_CLASS,
	pricing.CouponStatusNewRidersOnly:  pricingv1.CouponStatus_COUPON_STATUS_NEW_RIDERS_ONLY,
	pricing.CouponStatusBelowMinimum:   pricingv1.CouponStatus_COUPON_STATUS_BELOW_MINIMUM,
	pricing.CouponStatusBetterDiscount: pricingv1.CouponStatus_COUPON_STATUS_BETTER_DISCOUNT,
}

func toProtoCouponStatus(s pricing.CouponStatus) pricingv1.CouponStatus {
	return couponStatuses[s]
}

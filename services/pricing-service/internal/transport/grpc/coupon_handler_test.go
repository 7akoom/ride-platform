package grpc

import (
	"testing"
	"time"

	pricingv1 "github.com/7akoom/ride-platform/gen/go/ride/pricing/v1"
	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/pricing"
)

func TestEveryCouponStatusHasAWireValue(t *testing.T) {
	statuses := []pricing.CouponStatus{
		pricing.CouponStatusApplied, pricing.CouponStatusNotFound, pricing.CouponStatusEnded,
		pricing.CouponStatusNotStarted, pricing.CouponStatusExpired, pricing.CouponStatusUsedUp,
		pricing.CouponStatusAlreadyUsed, pricing.CouponStatusNotInArea, pricing.CouponStatusNotForClass,
		pricing.CouponStatusNewRidersOnly, pricing.CouponStatusBelowMinimum, pricing.CouponStatusBetterDiscount,
	}

	seen := map[pricingv1.CouponStatus]bool{}

	for _, s := range statuses {
		wire := toProtoCouponStatus(s)
		if wire == pricingv1.CouponStatus_COUPON_STATUS_UNSPECIFIED || seen[wire] {
			t.Fatalf("%q -> %s", s, wire)
		}

		seen[wire] = true
	}

	if len(seen) != len(pricingv1.CouponStatus_name)-1 {
		t.Fatalf("%d of %d wire values used", len(seen), len(pricingv1.CouponStatus_name)-1)
	}

	if toProtoCouponStatus(pricing.CouponStatusNone) != pricingv1.CouponStatus_COUPON_STATUS_UNSPECIFIED {
		t.Fatal("no code must be unspecified")
	}
}

func TestACouponOnTheWire(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	limit := decimal.NewFromInt(750)
	uses := 3

	got := toProtoCoupon(pricing.Coupon{
		Code: "SAVE", DiscountType: pricing.DiscountPercentage, DiscountValue: decimal.NewFromInt(20),
		MaxDiscountAmount: &limit, MaxRedemptions: &uses, RedemptionCount: 3, Active: true,
		ValidFrom: now.Add(-time.Hour), ValidUntil: now.Add(time.Hour), VehicleClasses: []string{"economy"},
	}, now)

	if got.GetMaxDiscountAmount() != "750" || got.GetMaxRedemptions() != 3 || got.GetState() != "used_up" ||
		got.GetDiscountType() != pricingv1.DiscountType_DISCOUNT_TYPE_PERCENTAGE || got.GetCreatedAt() != nil {
		t.Fatalf("got %+v", got)
	}

	if unlimited := toProtoCoupon(pricing.Coupon{Active: true, ValidUntil: now.Add(time.Hour)}, now); unlimited.GetMaxRedemptions() != 0 ||
		unlimited.GetMaxDiscountAmount() != "" || unlimited.GetState() != "running" {
		t.Fatalf("got %+v", unlimited)
	}
}

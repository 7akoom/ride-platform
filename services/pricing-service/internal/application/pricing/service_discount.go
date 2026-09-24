package pricing

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// promotions is what a rider's discounts depend on, read once per request
// whatever the number of classes priced.
type promotions struct {
	settings       PromotionSettings
	completedTrips int

	// code is the code the rider entered (upper-cased), empty for none;
	// coupon is that coupon when it exists, and riderUses how many of its
	// uses this rider holds.
	code        string
	coupon      Coupon
	couponFound bool
	riderUses   int
}

// NormalizeCouponCode is how a code is stored and looked up: trimmed and
// upper-case.
func NormalizeCouponCode(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}

func (s *service) readPromotions(ctx context.Context, riderID, couponCode string) (promotions, error) {
	settings, err := s.repository.GetPromotionSettings(ctx)
	if err != nil {
		return promotions{}, fmt.Errorf("read promotion settings: %w", err)
	}

	completed, err := s.repository.GetRiderCompletedTripCount(ctx, riderID)
	if err != nil {
		return promotions{}, fmt.Errorf("read the rider's completed trips: %w", err)
	}

	p := promotions{settings: settings, completedTrips: completed, code: NormalizeCouponCode(couponCode)}
	if p.code == "" {
		return p, nil
	}

	coupon, err := s.repository.FindCouponByCode(ctx, p.code)

	switch {
	case errors.Is(err, ErrCouponNotFound):
		// An unknown code never fails the price: the rider is told.
		return p, nil
	case err != nil:
		return promotions{}, fmt.Errorf("read coupon: %w", err)
	}

	uses, err := s.repository.RiderRedemptionCount(ctx, coupon.ID, riderID)
	if err != nil {
		return promotions{}, fmt.Errorf("count the rider's coupon uses: %w", err)
	}

	p.coupon, p.couponFound, p.riderUses = coupon, true, uses

	return p, nil
}

type selectedDiscount struct {
	Type   DiscountType
	Label  string
	Amount decimal.Decimal
	Coupon *AppliedCoupon // set only when a coupon was the winning discount
	// CouponStatus is what became of the rider's code.
	CouponStatus CouponStatus
}

// selectBestDiscount evaluates every discount the rider might qualify
// for and returns only the single largest one — coupon, first-ride, and
// loyalty discounts are never stacked together. This is a deliberate
// anti-abuse choice: stacking would let a promo-hunting rider combine a
// coupon with a first-ride discount for a near-free trip. A coupon wins a
// tie.
func selectBestDiscount(p promotions, zone ServiceZone, class string, chargeable decimal.Decimal, now time.Time) selectedDiscount {
	best := selectedDiscount{Type: DiscountNone, Amount: decimal.Zero}

	couponAmount, status := couponDiscount(p, zone, class, chargeable, now)
	best.CouponStatus = status

	if status == CouponStatusApplied {
		best.Type = p.coupon.DiscountType
		best.Label = "Coupon: " + p.coupon.Code
		best.Amount = couponAmount
		best.Coupon = &AppliedCoupon{CouponID: p.coupon.ID, DiscountAmount: couponAmount}
	}

	label, automatic := automaticDiscount(p, chargeable)
	if automatic.GreaterThan(best.Amount) {
		if status == CouponStatusApplied {
			best.CouponStatus = CouponStatusBetterDiscount
		}

		best.Type = DiscountPercentage
		best.Label = label
		best.Amount = automatic
		best.Coupon = nil
	}

	return best
}

// couponDiscount is what the rider's coupon takes off, and why nothing
// when it takes nothing off.
func couponDiscount(p promotions, zone ServiceZone, class string, chargeable decimal.Decimal, now time.Time) (decimal.Decimal, CouponStatus) {
	if p.code == "" {
		return decimal.Zero, CouponStatusNone
	}

	if !p.couponFound {
		return decimal.Zero, CouponStatusNotFound
	}

	c := p.coupon

	switch c.State(now) {
	case CouponStateEnded:
		return decimal.Zero, CouponStatusEnded
	case CouponStateExpired:
		return decimal.Zero, CouponStatusExpired
	case CouponStateUsedUp:
		return decimal.Zero, CouponStatusUsedUp
	case CouponStateScheduled:
		return decimal.Zero, CouponStatusNotStarted
	}

	switch {
	case c.ZoneID != "" && c.ZoneID != zone.ZoneID, c.CityID != "" && c.CityID != zone.CityID:
		return decimal.Zero, CouponStatusNotInArea
	case !c.AppliesToClass(class):
		return decimal.Zero, CouponStatusNotForClass
	case c.NewRidersOnly && p.completedTrips > 0:
		return decimal.Zero, CouponStatusNewRidersOnly
	case p.riderUses >= c.PerRiderLimit:
		return decimal.Zero, CouponStatusAlreadyUsed
	case chargeable.LessThan(c.MinimumFareAmount):
		return decimal.Zero, CouponStatusBelowMinimum
	}

	amount := decimal.Zero

	switch c.DiscountType {
	case DiscountPercentage:
		amount = capped(chargeable.Mul(c.DiscountValue).Div(hundred), c.MaxDiscountAmount)
	case DiscountFixed:
		amount = c.DiscountValue
	}

	return decimal.Min(amount, chargeable), CouponStatusApplied
}

// automaticDiscount is the first-ride or the loyalty discount the rider
// has on this trip, if any.
func automaticDiscount(p promotions, chargeable decimal.Decimal) (string, decimal.Decimal) {
	settings := p.settings

	if p.completedTrips == 0 {
		if !settings.FirstRidePercent.IsPositive() {
			return "", decimal.Zero
		}

		amount := capped(chargeable.Mul(settings.FirstRidePercent).Div(hundred), settings.FirstRideMaxAmount)

		return "First ride discount", decimal.Min(amount, chargeable)
	}

	// +1: this fare is for the trip about to complete, not counted yet —
	// "is this the Nth one".
	if settings.LoyaltyEvery > 0 && settings.LoyaltyPercent.IsPositive() && (p.completedTrips+1)%settings.LoyaltyEvery == 0 {
		amount := capped(chargeable.Mul(settings.LoyaltyPercent).Div(hundred), settings.LoyaltyMaxAmount)

		return "Loyalty discount", decimal.Min(amount, chargeable)
	}

	return "", decimal.Zero
}

func capped(amount decimal.Decimal, limit *decimal.Decimal) decimal.Decimal {
	if limit != nil && amount.GreaterThan(*limit) {
		return *limit
	}

	return amount
}

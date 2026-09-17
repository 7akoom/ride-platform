package pricing

import (
	"context"
	"strings"

	"github.com/shopspring/decimal"
)

// These are simple fixed policies for v1 — a real system would likely
// make them configurable per deployment the same way pricing_configs
// is, but that's more surface area than this milestone needs. Easy to
// find and adjust here, or promote to a config table later.
var (
	firstRideDiscountPercent = decimal.NewFromInt(50)
	loyaltyDiscountPercent   = decimal.NewFromInt(20)
)

const loyaltyRideInterval = 10 // every 10th completed ride

type selectedDiscount struct {
	Type   DiscountType
	Label  string
	Amount decimal.Decimal
	Coupon *AppliedCoupon // set only when a coupon was the winning discount
}

// selectBestDiscount evaluates every discount the rider might qualify
// for and returns only the single largest one — coupon, first-ride, and
// loyalty discounts are never stacked together. This is a deliberate
// anti-abuse choice: stacking would let a promo-hunting rider combine a
// coupon with a first-ride discount for a near-free trip.
func (s *service) selectBestDiscount(
	ctx context.Context,
	riderID string,
	couponCode string,
	chargeableAmount decimal.Decimal,
) (selectedDiscount, error) {
	best := selectedDiscount{Type: DiscountNone, Amount: decimal.Zero}

	if trimmed := strings.TrimSpace(couponCode); trimmed != "" {
		candidate, err := s.evaluateCoupon(ctx, trimmed, riderID, chargeableAmount)
		if err != nil {
			return selectedDiscount{}, err
		}

		if candidate.Amount.GreaterThan(best.Amount) {
			best = candidate
		}
	}

	completedTrips, err := s.repository.GetRiderCompletedTripCount(ctx, riderID)
	if err != nil {
		return selectedDiscount{}, err
	}

	if completedTrips == 0 {
		amount := chargeableAmount.Mul(firstRideDiscountPercent).Div(decimal.NewFromInt(100))

		if amount.GreaterThan(best.Amount) {
			best = selectedDiscount{
				Type:   DiscountPercentage,
				Label:  "First ride discount",
				Amount: amount,
			}
		}
	} else if (completedTrips+1)%loyaltyRideInterval == 0 {
		// +1 because this fare is for the trip currently being
		// completed, which isn't counted yet — this check is "is the
		// trip about to complete the Nth one".
		amount := chargeableAmount.Mul(loyaltyDiscountPercent).Div(decimal.NewFromInt(100))

		if amount.GreaterThan(best.Amount) {
			best = selectedDiscount{
				Type:   DiscountPercentage,
				Label:  "Loyalty discount",
				Amount: amount,
			}
		}
	}

	return best, nil
}

func (s *service) evaluateCoupon(
	ctx context.Context,
	code string,
	riderID string,
	chargeableAmount decimal.Decimal,
) (selectedDiscount, error) {
	coupon, err := s.repository.FindCouponByCode(ctx, code)
	if err != nil {
		return selectedDiscount{}, err
	}

	if !coupon.IsCurrentlyValid(nowFunc()) {
		return selectedDiscount{}, nil
	}

	if chargeableAmount.LessThan(coupon.MinimumFareAmount) {
		return selectedDiscount{}, nil
	}

	riderUses, err := s.repository.RiderRedemptionCount(ctx, coupon.ID, riderID)
	if err != nil {
		return selectedDiscount{}, err
	}

	if riderUses >= coupon.PerRiderLimit {
		return selectedDiscount{}, nil
	}

	amount := decimal.Zero

	switch coupon.DiscountType {
	case DiscountPercentage:
		amount = chargeableAmount.Mul(coupon.DiscountValue).Div(decimal.NewFromInt(100))
	case DiscountFixed:
		amount = coupon.DiscountValue
	}

	if amount.GreaterThan(chargeableAmount) {
		amount = chargeableAmount
	}

	return selectedDiscount{
		Type:   coupon.DiscountType,
		Label:  "Coupon: " + coupon.Code,
		Amount: amount,
		Coupon: &AppliedCoupon{
			CouponID:       coupon.ID,
			DiscountAmount: amount,
		},
	}, nil
}

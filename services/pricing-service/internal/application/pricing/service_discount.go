package pricing

import (
	"context"
	"strings"
)

// These are simple fixed policies for v1 — a real system would likely
// make them configurable per deployment the same way pricing_configs
// is, but that's more surface area than this milestone needs. Easy to
// find and adjust here, or promote to a config table later.
const (
	firstRideDiscountPercent = 50.0
	loyaltyRideInterval      = 10 // every 10th completed ride
	loyaltyDiscountPercent   = 20.0
)

type selectedDiscount struct {
	Type   DiscountType
	Label  string
	Amount float64
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
	chargeableAmount float64,
) (selectedDiscount, error) {
	best := selectedDiscount{Type: DiscountNone}

	if trimmed := strings.TrimSpace(couponCode); trimmed != "" {
		candidate, err := s.evaluateCoupon(ctx, trimmed, riderID, chargeableAmount)
		if err != nil {
			return selectedDiscount{}, err
		}

		if candidate.Amount > best.Amount {
			best = candidate
		}
	}

	completedTrips, err := s.repository.GetRiderCompletedTripCount(ctx, riderID)
	if err != nil {
		return selectedDiscount{}, err
	}

	if completedTrips == 0 {
		amount := chargeableAmount * firstRideDiscountPercent / 100

		if amount > best.Amount {
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
		amount := chargeableAmount * loyaltyDiscountPercent / 100

		if amount > best.Amount {
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
	chargeableAmount float64,
) (selectedDiscount, error) {
	coupon, err := s.repository.FindCouponByCode(ctx, code)
	if err != nil {
		return selectedDiscount{}, err
	}

	if !coupon.IsCurrentlyValid(nowFunc()) {
		return selectedDiscount{}, nil
	}

	if chargeableAmount < coupon.MinimumFareAmount {
		return selectedDiscount{}, nil
	}

	riderUses, err := s.repository.RiderRedemptionCount(ctx, coupon.ID, riderID)
	if err != nil {
		return selectedDiscount{}, err
	}

	if riderUses >= coupon.PerRiderLimit {
		return selectedDiscount{}, nil
	}

	var amount float64

	switch coupon.DiscountType {
	case DiscountPercentage:
		amount = chargeableAmount * coupon.DiscountValue / 100
	case DiscountFixed:
		amount = coupon.DiscountValue
	}

	if amount > chargeableAmount {
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

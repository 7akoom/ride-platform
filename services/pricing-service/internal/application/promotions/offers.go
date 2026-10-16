package promotions

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/pricing"
)

// ErrRiderRequired: a rider's offers are asked for a rider.
var ErrRiderRequired = errors.New("rider_id is required")

// maxFeaturedOffers is how many featured coupons the app's home lists.
const maxFeaturedOffers = 20

// CouponOffer is a coupon the rider may use, with how many more of their
// trips it may take off.
type CouponOffer struct {
	Coupon   pricing.Coupon
	UsesLeft int
}

// AutomaticOffer is a discount without a code: a percent, capped or not.
type AutomaticOffer struct {
	Percent   decimal.Decimal
	MaxAmount *decimal.Decimal
}

// LoyaltyOffer: every Nth completed trip is discounted. TripsToGo counts
// the discounted trip: 1 means the rider's next trip.
type LoyaltyOffer struct {
	AutomaticOffer
	Every          int
	CompletedTrips int
	TripsToGo      int
}

// RiderOffers is what the app's home shows. FirstRide is set for a rider
// who never completed a trip, Loyalty for one who did, each while staff
// have it on.
type RiderOffers struct {
	Coupons   []CouponOffer
	FirstRide *AutomaticOffer
	Loyalty   *LoyaltyOffer
}

// CouponCheck is what a typed code is to the rider: CouponStatusApplied and
// the offer when they may use it, otherwise why not.
type CouponCheck struct {
	Status pricing.CouponStatus
	Offer  *CouponOffer
}

// RiderOffers lists the featured coupons the rider may still use (not used
// up for them, not for new riders only once they completed a trip) and
// their automatic discounts. Where a coupon applies (area, class, minimum
// fare) is the quote's to check: the rider has no trip yet.
func (s *Service) RiderOffers(ctx context.Context, riderID string) (RiderOffers, error) {
	riderID = strings.TrimSpace(riderID)
	if riderID == "" {
		return RiderOffers{}, ErrRiderRequired
	}

	completed, err := s.repository.GetRiderCompletedTripCount(ctx, riderID)
	if err != nil {
		return RiderOffers{}, fmt.Errorf("read the rider's completed trips: %w", err)
	}

	settings, err := s.repository.GetPromotionSettings(ctx)
	if err != nil {
		return RiderOffers{}, fmt.Errorf("read promotion settings: %w", err)
	}

	coupons, err := s.repository.ListFeaturedCoupons(ctx, s.now(), maxFeaturedOffers)
	if err != nil {
		return RiderOffers{}, fmt.Errorf("list featured coupons: %w", err)
	}

	offers := RiderOffers{Coupons: []CouponOffer{}}

	for _, coupon := range coupons {
		check, err := s.couponFor(ctx, coupon, riderID, completed)
		if err != nil {
			return RiderOffers{}, err
		}

		if check.Offer != nil {
			offers.Coupons = append(offers.Coupons, *check.Offer)
		}
	}

	offers.FirstRide, offers.Loyalty = automaticOffers(settings, completed)

	return offers, nil
}

// CheckCoupon says whether the rider may use the code now, featured or
// not: the "enter a code" box on the app's home.
func (s *Service) CheckCoupon(ctx context.Context, riderID, code string) (CouponCheck, error) {
	riderID = strings.TrimSpace(riderID)
	if riderID == "" {
		return CouponCheck{}, ErrRiderRequired
	}

	code = pricing.NormalizeCouponCode(code)
	if !codePattern.MatchString(code) {
		return CouponCheck{Status: pricing.CouponStatusNotFound}, nil
	}

	coupon, err := s.repository.FindCouponByCode(ctx, code)

	switch {
	case errors.Is(err, pricing.ErrCouponNotFound):
		return CouponCheck{Status: pricing.CouponStatusNotFound}, nil
	case err != nil:
		return CouponCheck{}, fmt.Errorf("read coupon: %w", err)
	}

	completed, err := s.repository.GetRiderCompletedTripCount(ctx, riderID)
	if err != nil {
		return CouponCheck{}, fmt.Errorf("read the rider's completed trips: %w", err)
	}

	return s.couponFor(ctx, coupon, riderID, completed)
}

// couponFor is what the coupon is to the rider now, as a quote would say
// before it looks at the trip.
func (s *Service) couponFor(ctx context.Context, coupon pricing.Coupon, riderID string, completed int) (CouponCheck, error) {
	switch coupon.State(s.now()) {
	case pricing.CouponStateEnded:
		return CouponCheck{Status: pricing.CouponStatusEnded}, nil
	case pricing.CouponStateExpired:
		return CouponCheck{Status: pricing.CouponStatusExpired}, nil
	case pricing.CouponStateUsedUp:
		return CouponCheck{Status: pricing.CouponStatusUsedUp}, nil
	case pricing.CouponStateScheduled:
		return CouponCheck{Status: pricing.CouponStatusNotStarted}, nil
	}

	if coupon.NewRidersOnly && completed > 0 {
		return CouponCheck{Status: pricing.CouponStatusNewRidersOnly}, nil
	}

	uses, err := s.repository.RiderRedemptionCount(ctx, coupon.ID, riderID)
	if err != nil {
		return CouponCheck{}, fmt.Errorf("count the rider's coupon uses: %w", err)
	}

	if uses >= coupon.PerRiderLimit {
		return CouponCheck{Status: pricing.CouponStatusAlreadyUsed}, nil
	}

	return CouponCheck{
		Status: pricing.CouponStatusApplied,
		Offer:  &CouponOffer{Coupon: coupon, UsesLeft: coupon.PerRiderLimit - uses},
	}, nil
}

// automaticOffers is the first-ride discount (no completed trip yet) or the
// loyalty one (how far the next discounted trip is), as pricing applies
// them.
func automaticOffers(settings pricing.PromotionSettings, completed int) (*AutomaticOffer, *LoyaltyOffer) {
	if completed == 0 {
		if !settings.FirstRidePercent.IsPositive() {
			return nil, nil
		}

		return &AutomaticOffer{Percent: settings.FirstRidePercent, MaxAmount: settings.FirstRideMaxAmount}, nil
	}

	if settings.LoyaltyEvery <= 0 || !settings.LoyaltyPercent.IsPositive() {
		return nil, nil
	}

	// The next trip number that is a multiple of every.
	next := (completed/settings.LoyaltyEvery + 1) * settings.LoyaltyEvery

	return nil, &LoyaltyOffer{
		AutomaticOffer: AutomaticOffer{Percent: settings.LoyaltyPercent, MaxAmount: settings.LoyaltyMaxAmount},
		Every:          settings.LoyaltyEvery,
		CompletedTrips: completed,
		TripsToGo:      next - completed,
	}
}

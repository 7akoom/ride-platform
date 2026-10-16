package promotions

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/pricing"
)

const riderID = "44444444-4444-4444-8444-444444444444"

// runningCoupon is on offer at now: one use per rider unless changed.
func runningCoupon(code string) pricing.Coupon {
	return pricing.Coupon{
		ID:            "coupon-" + code,
		Code:          code,
		DiscountType:  pricing.DiscountPercentage,
		DiscountValue: dec("30"),
		ValidFrom:     now.Add(-time.Hour),
		ValidUntil:    now.Add(7 * 24 * time.Hour),
		PerRiderLimit: 1,
		Featured:      true,
		Active:        true,
	}
}

func TestARiderSeesTheFeaturedCouponsTheyMayUse(t *testing.T) {
	service, repo := newTestService()
	repo.completed = 3

	twice := runningCoupon("TWICE")
	twice.PerRiderLimit = 2
	newOnly := runningCoupon("NEW")
	newOnly.NewRidersOnly = true
	used := runningCoupon("USED")
	ended := runningCoupon("ENDED")
	ended.Active = false

	repo.featured = []pricing.Coupon{runningCoupon("SAVE30"), twice, newOnly, used, ended}
	repo.uses = map[string]int{twice.ID: 1, used.ID: 1}

	offers, err := service.RiderOffers(context.Background(), riderID)
	if err != nil {
		t.Fatal(err)
	}

	if len(offers.Coupons) != 2 || offers.Coupons[0].Coupon.Code != "SAVE30" || offers.Coupons[0].UsesLeft != 1 ||
		offers.Coupons[1].Coupon.Code != "TWICE" || offers.Coupons[1].UsesLeft != 1 {
		t.Fatalf("coupons %+v", offers.Coupons)
	}
}

func TestTheFirstRideDiscountIsForARiderWithNoTrips(t *testing.T) {
	service, repo := newTestService()
	repo.settings = pricing.PromotionSettings{
		FirstRidePercent: dec("50"), FirstRideMaxAmount: ptr(dec("5000")),
		LoyaltyEvery: 10, LoyaltyPercent: dec("20"),
	}

	offers, err := service.RiderOffers(context.Background(), riderID)
	if err != nil {
		t.Fatal(err)
	}

	if offers.FirstRide == nil || !offers.FirstRide.Percent.Equal(dec("50")) || !offers.FirstRide.MaxAmount.Equal(dec("5000")) ||
		offers.Loyalty != nil {
		t.Fatalf("first ride %+v loyalty %+v", offers.FirstRide, offers.Loyalty)
	}

	// Turned off: nothing to show.
	repo.settings.FirstRidePercent = dec("0")

	if offers, _ := service.RiderOffers(context.Background(), riderID); offers.FirstRide != nil {
		t.Fatalf("first ride off: %+v", offers.FirstRide)
	}
}

func TestLoyaltySaysHowFarTheNextDiscountedTripIs(t *testing.T) {
	cases := map[int]int{1: 9, 8: 2, 9: 1, 10: 10, 19: 1}

	for completed, toGo := range cases {
		service, repo := newTestService()
		repo.settings = pricing.PromotionSettings{FirstRidePercent: dec("50"), LoyaltyEvery: 10, LoyaltyPercent: dec("20")}
		repo.completed = completed

		offers, err := service.RiderOffers(context.Background(), riderID)
		if err != nil {
			t.Fatal(err)
		}

		if offers.FirstRide != nil || offers.Loyalty == nil || offers.Loyalty.TripsToGo != toGo ||
			offers.Loyalty.CompletedTrips != completed || offers.Loyalty.Every != 10 {
			t.Fatalf("%d completed: loyalty %+v", completed, offers.Loyalty)
		}
	}

	service, repo := newTestService()
	repo.settings = pricing.PromotionSettings{LoyaltyEvery: 0, LoyaltyPercent: dec("20")}
	repo.completed = 4

	if offers, _ := service.RiderOffers(context.Background(), riderID); offers.Loyalty != nil {
		t.Fatalf("loyalty off: %+v", offers.Loyalty)
	}
}

func TestCheckingATypedCode(t *testing.T) {
	service, repo := newTestService()
	repo.completed = 2

	open := runningCoupon("OPEN10")
	open.Featured = false
	scheduled := runningCoupon("SOON")
	scheduled.ValidFrom = now.Add(time.Hour)
	expired := runningCoupon("OLD")
	expired.ValidUntil = now.Add(-time.Hour)
	expired.ValidFrom = now.Add(-48 * time.Hour)
	full := runningCoupon("FULL")
	full.MaxRedemptions, full.RedemptionCount = ptr(5), 5
	newOnly := runningCoupon("NEW")
	newOnly.NewRidersOnly = true
	used := runningCoupon("USED")

	for _, c := range []pricing.Coupon{open, scheduled, expired, full, newOnly, used} {
		repo.coupons[c.Code] = c
	}

	repo.uses = map[string]int{used.ID: 1}

	cases := map[string]pricing.CouponStatus{
		" open10 ": pricing.CouponStatusApplied,
		"SOON":     pricing.CouponStatusNotStarted,
		"OLD":      pricing.CouponStatusExpired,
		"FULL":     pricing.CouponStatusUsedUp,
		"NEW":      pricing.CouponStatusNewRidersOnly,
		"USED":     pricing.CouponStatusAlreadyUsed,
		"NOPE":     pricing.CouponStatusNotFound,
		"x":        pricing.CouponStatusNotFound,
	}

	for code, want := range cases {
		check, err := service.CheckCoupon(context.Background(), riderID, code)
		if err != nil {
			t.Fatal(err)
		}

		if check.Status != want || (want == pricing.CouponStatusApplied) != (check.Offer != nil) {
			t.Fatalf("%q: %+v, want %v", code, check, want)
		}
	}
}

func TestOffersAreForARider(t *testing.T) {
	service, _ := newTestService()

	if _, err := service.RiderOffers(context.Background(), " "); !errors.Is(err, ErrRiderRequired) {
		t.Fatalf("offers: %v", err)
	}

	if _, err := service.CheckCoupon(context.Background(), "", "SAVE30"); !errors.Is(err, ErrRiderRequired) {
		t.Fatalf("check: %v", err)
	}
}

func TestAFeaturedCouponIsMadeAndChanged(t *testing.T) {
	service, repo := newTestService()

	input := validCoupon()
	input.Featured = true

	created, err := service.CreateCoupon(context.Background(), input, staffID)
	if err != nil || !created.Featured {
		t.Fatalf("created %+v %v", created, err)
	}

	if _, err := service.UpdateCoupon(context.Background(), created.Code, CouponChange{Featured: ptr(false)}, staffID); err != nil {
		t.Fatal(err)
	}

	if change := repo.changes[0]; change.Featured == nil || *change.Featured {
		t.Fatalf("change %+v", change)
	}
}

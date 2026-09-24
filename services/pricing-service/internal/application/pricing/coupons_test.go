package pricing

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

var couponNow = time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)

func runningCoupon() Coupon {
	return Coupon{
		ID: "coupon-1", Code: "SAVE20", DiscountType: DiscountPercentage, DiscountValue: decimal.NewFromInt(20),
		Active: true, ValidFrom: couponNow.Add(-time.Hour), ValidUntil: couponNow.Add(time.Hour), PerRiderLimit: 1,
	}
}

func dec(v int64) decimal.Decimal { return decimal.NewFromInt(v) }

func TestWhatBecomesOfACoupon(t *testing.T) {
	zone := ServiceZone{Served: true, ZoneID: "zone-1", CityID: "city-1"}
	max2 := 2

	cases := []struct {
		name      string
		change    func(p *promotions)
		class     string
		status    CouponStatus
		discount  int64
		winsLabel string
	}{
		{"no code", func(p *promotions) { p.code = "" }, "economy", CouponStatusNone, 0, ""},
		{"unknown code", func(p *promotions) { p.couponFound = false }, "economy", CouponStatusNotFound, 0, ""},
		{"turned off", func(p *promotions) { p.coupon.Active = false }, "economy", CouponStatusEnded, 0, ""},
		{"not started", func(p *promotions) { p.coupon.ValidFrom = couponNow.Add(time.Minute) }, "economy", CouponStatusNotStarted, 0, ""},
		{"expired", func(p *promotions) { p.coupon.ValidUntil = couponNow.Add(-time.Minute) }, "economy", CouponStatusExpired, 0, ""},
		{"used up", func(p *promotions) { p.coupon.MaxRedemptions = &max2; p.coupon.RedemptionCount = 2 }, "economy", CouponStatusUsedUp, 0, ""},
		{"another zone", func(p *promotions) { p.coupon.ZoneID = "zone-2" }, "economy", CouponStatusNotInArea, 0, ""},
		{"another city", func(p *promotions) { p.coupon.CityID = "city-2" }, "economy", CouponStatusNotInArea, 0, ""},
		{"its zone", func(p *promotions) { p.coupon.ZoneID = "zone-1" }, "economy", CouponStatusApplied, 1000, "Coupon: SAVE20"},
		{"another class", func(p *promotions) { p.coupon.VehicleClasses = []string{"comfort"} }, "economy", CouponStatusNotForClass, 0, ""},
		{"its class", func(p *promotions) { p.coupon.VehicleClasses = []string{"comfort"} }, "comfort", CouponStatusApplied, 1000, "Coupon: SAVE20"},
		{"new riders only", func(p *promotions) { p.coupon.NewRidersOnly = true }, "economy", CouponStatusNewRidersOnly, 0, ""},
		{"used by the rider", func(p *promotions) { p.riderUses = 1 }, "economy", CouponStatusAlreadyUsed, 0, ""},
		{"below its minimum", func(p *promotions) { p.coupon.MinimumFareAmount = dec(6000) }, "economy", CouponStatusBelowMinimum, 0, ""},
		{"capped", func(p *promotions) { cap := dec(600); p.coupon.MaxDiscountAmount = &cap }, "economy", CouponStatusApplied, 600, "Coupon: SAVE20"},
		{"a fixed amount", func(p *promotions) { p.coupon.DiscountType = DiscountFixed; p.coupon.DiscountValue = dec(750) }, "economy", CouponStatusApplied, 750, "Coupon: SAVE20"},
		{"more than the fare", func(p *promotions) { p.coupon.DiscountType = DiscountFixed; p.coupon.DiscountValue = dec(9000) }, "economy", CouponStatusApplied, 5000, "Coupon: SAVE20"},
		{"the first ride gives more", func(p *promotions) { p.completedTrips = 0 }, "economy", CouponStatusBetterDiscount, 2500, "First ride discount"},
		{"the tenth ride gives the same", func(p *promotions) { p.completedTrips = 9 }, "economy", CouponStatusApplied, 1000, "Coupon: SAVE20"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := promotions{
				settings: DefaultPromotionSettings(), completedTrips: 3,
				code: "SAVE20", coupon: runningCoupon(), couponFound: true,
			}
			tc.change(&p)

			got := selectBestDiscount(p, zone, tc.class, dec(5000), couponNow)

			if got.CouponStatus != tc.status || !got.Amount.Equal(dec(tc.discount)) || got.Label != tc.winsLabel {
				t.Fatalf("got %s %s %q", got.CouponStatus, got.Amount, got.Label)
			}

			if (got.Coupon != nil) != (tc.winsLabel == "Coupon: SAVE20") {
				t.Fatalf("applied coupon %+v", got.Coupon)
			}
		})
	}
}

func TestTheAutomaticDiscountsFollowTheSettings(t *testing.T) {
	cap := dec(1000)

	cases := []struct {
		name      string
		settings  PromotionSettings
		completed int
		label     string
		amount    int64
	}{
		{"first ride", DefaultPromotionSettings(), 0, "First ride discount", 2500},
		{"first ride, capped", PromotionSettings{FirstRidePercent: dec(50), FirstRideMaxAmount: &cap}, 0, "First ride discount", 1000},
		{"first ride off", PromotionSettings{LoyaltyEvery: 10, LoyaltyPercent: dec(20)}, 0, "", 0},
		{"the 10th trip", DefaultPromotionSettings(), 9, "Loyalty discount", 1000},
		{"the 11th trip", DefaultPromotionSettings(), 10, "", 0},
		{"every 5th, capped", PromotionSettings{LoyaltyEvery: 5, LoyaltyPercent: dec(30), LoyaltyMaxAmount: &cap}, 4, "Loyalty discount", 1000},
		{"loyalty off", PromotionSettings{FirstRidePercent: dec(50), LoyaltyEvery: 0, LoyaltyPercent: dec(20)}, 9, "", 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := selectBestDiscount(promotions{settings: tc.settings, completedTrips: tc.completed}, ServiceZone{}, "economy", dec(5000), couponNow)
			if got.Label != tc.label || !got.Amount.Equal(dec(tc.amount)) || got.CouponStatus != CouponStatusNone {
				t.Fatalf("got %q %s", got.Label, got.Amount)
			}
		})
	}
}

func TestAQuoteSaysWhatBecameOfTheCodeForEachClass(t *testing.T) {
	h := newHarness()
	h.repo.completedTripCount = 3
	coupon := runningCoupon()
	coupon.VehicleClasses = []string{VehicleClassEconomy}
	h.repo.couponsByCode["SAVE20"] = coupon
	withFrozenTime(t, couponNow)

	input := quoteInput()
	input.CouponCode = " save20 "

	got, err := h.service().QuoteTrip(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}

	for _, quote := range got.Quotes {
		want := CouponStatusNotForClass
		if quote.VehicleClass == VehicleClassEconomy {
			want = CouponStatusApplied
		}

		if quote.Breakdown.CouponStatus != want || (quote.Coupon != nil) != (want == CouponStatusApplied) {
			t.Fatalf("%s: %s %+v", quote.VehicleClass, quote.Breakdown.CouponStatus, quote.Coupon)
		}
	}
}

func TestAClaimedQuoteHoldsItsCouponUntilTheTripIsCancelled(t *testing.T) {
	h := newHarness()
	h.repo.completedTripCount = 3
	h.repo.couponsByCode["SAVE20"] = runningCoupon()
	withFrozenTime(t, couponNow)
	svc := h.service()

	input := quoteInput()
	input.CouponCode = "SAVE20"

	first, err := svc.QuoteTrip(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.ClaimQuote(context.Background(), first.Quotes[0].ID, riderA, tripA); err != nil {
		t.Fatal(err)
	}

	// The rider's one use is held: a new quote does not apply the code.
	second, err := svc.QuoteTrip(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}

	if status := second.Quotes[0].Breakdown.CouponStatus; status != CouponStatusAlreadyUsed {
		t.Fatalf("status %s", status)
	}

	// The trip is cancelled: the use is free again.
	if err := svc.ReleaseTripCoupon(context.Background(), tripA); err != nil {
		t.Fatal(err)
	}

	third, err := svc.QuoteTrip(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}

	if status := third.Quotes[0].Breakdown.CouponStatus; status != CouponStatusApplied {
		t.Fatalf("status %s", status)
	}

	if len(h.repo.releasedTrips) != 1 || h.repo.releasedTrips[0] != tripA {
		t.Fatalf("released %v", h.repo.releasedTrips)
	}
}

func TestReleasingATripCouponIgnoresOddIDs(t *testing.T) {
	h := newHarness()
	svc := h.service()

	if err := svc.ReleaseTripCoupon(context.Background(), " "); err != ErrTripIDRequired {
		t.Fatalf("got %v", err)
	}

	if err := svc.ReleaseTripCoupon(context.Background(), "not-a-uuid"); err != nil || len(h.repo.releasedTrips) != 0 {
		t.Fatalf("got %v, %v", err, h.repo.releasedTrips)
	}
}

func TestATripWhoseCouponRanOutIsChargedWithoutIt(t *testing.T) {
	h := newHarness()
	h.repo.completedTripCount = 3
	h.repo.couponsByCode["SAVE20"] = runningCoupon()
	withFrozenTime(t, couponNow)
	svc := h.service(WithFareRounding(dec(250)))

	input := quoteInput()
	input.CouponCode = "SAVE20"

	quotes, err := svc.QuoteTrip(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}

	quote := quotes.Quotes[0]
	if _, err := svc.ClaimQuote(context.Background(), quote.ID, riderA, tripA); err != nil {
		t.Fatal(err)
	}

	// The trip held no use the repository could redeem.
	h.repo.couponUnavailable = true

	fare, err := svc.CalculateFare(context.Background(), CalculateFareInput{TripID: tripA, RiderID: riderA, QuoteID: quote.ID})
	if err != nil {
		t.Fatal(err)
	}

	b := fare.Breakdown
	if len(h.repo.persistFareCalls) != 2 || h.repo.persistFareCalls[1].Coupon != nil {
		t.Fatalf("calls %+v", h.repo.persistFareCalls)
	}

	if !b.DiscountAmount.IsZero() || b.AppliedDiscountType != DiscountNone || b.CouponStatus != CouponStatusUsedUp ||
		!b.Total.Equal(roundToIncrement(b.Subtotal.Add(b.SurgeAmount), dec(250))) {
		t.Fatalf("breakdown %+v", b)
	}
}

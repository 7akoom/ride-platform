package postgres

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/pricing"
	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/promotions"
)

const testStaff = "99999999-9999-4999-8999-999999999999"

func testCoupon(code string, maxRedemptions *int) pricing.Coupon {
	now := time.Now().UTC()
	limit := decimal.NewFromInt(2000)

	return pricing.Coupon{
		Code: code, Description: "Autumn", DiscountType: pricing.DiscountPercentage, DiscountValue: decimal.NewFromInt(20),
		MaxDiscountAmount: &limit, ValidFrom: now.Add(-time.Hour), ValidUntil: now.Add(24 * time.Hour),
		MaxRedemptions: maxRedemptions, PerRiderLimit: 1, CityID: testCity, VehicleClasses: []string{"economy"},
		Active: true, CreatedBy: testStaff,
	}
}

func riderN(n int) string { return fmt.Sprintf("cccccccc-cccc-4ccc-8ccc-%012d", n) }
func tripN(n int) string  { return fmt.Sprintf("dddddddd-dddd-4ddd-8ddd-%012d", n) }

// quoteWithCoupon saves a quote for the rider priced with the coupon.
func quoteWithCoupon(t *testing.T, repo *PricingRepository, rider string, coupon pricing.Coupon) pricing.Quote {
	t.Helper()

	quote := quoteFor(rider, time.Now().UTC().Add(5*time.Minute))
	quote.Coupon = &pricing.AppliedCoupon{CouponID: coupon.ID, DiscountAmount: decimal.NewFromInt(800)}

	saved, err := repo.SaveQuotes(context.Background(), []pricing.Quote{quote})
	if err != nil {
		t.Fatal(err)
	}

	return saved[0]
}

func TestCouponsAreStoredWithWhereTheyApply(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewPricingRepository(pool)

	one := 1
	created, err := repo.CreateCoupon(ctx, testCoupon("AUTUMN", &one))
	if err != nil {
		t.Fatal(err)
	}

	if created.ID == "" || created.MaxDiscountAmount == nil || !created.MaxDiscountAmount.Equal(decimal.NewFromInt(2000)) ||
		created.CityID != testCity || len(created.VehicleClasses) != 1 || created.CreatedBy != testStaff || created.UpdatedBy != testStaff {
		t.Fatalf("created %+v", created)
	}

	if _, err := repo.CreateCoupon(ctx, testCoupon("AUTUMN", nil)); !errors.Is(err, pricing.ErrCouponAlreadyExists) {
		t.Fatalf("a taken code: %v", err)
	}

	if _, err := repo.FindCouponByCode(ctx, "NOPE"); !errors.Is(err, pricing.ErrCouponNotFound) {
		t.Fatalf("unknown: %v", err)
	}

	later := created.ValidUntil.Add(time.Hour)
	zero, off := 0, false

	updated, err := repo.UpdateCoupon(ctx, "AUTUMN", promotions.CouponChange{
		ValidUntil: &later, MaxRedemptions: &zero, Active: &off,
	}, testStaff)
	if err != nil || updated.MaxRedemptions != nil || updated.Active || !updated.ValidUntil.Equal(later) ||
		updated.Description != "Autumn" || updated.PerRiderLimit != 1 {
		t.Fatalf("updated %+v %v", updated, err)
	}

	if _, err := repo.UpdateCoupon(ctx, "NOPE", promotions.CouponChange{}, testStaff); !errors.Is(err, pricing.ErrCouponNotFound) {
		t.Fatalf("unknown: %v", err)
	}

	if _, err := repo.CreateCoupon(ctx, testCoupon("AUTUMN2", nil)); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()

	for _, tc := range []struct {
		state, prefix string
		want          []string
	}{
		{"", "", []string{"AUTUMN2", "AUTUMN"}},
		{promotions.StateRunning, "", []string{"AUTUMN2"}},
		{promotions.StateFinished, "", []string{"AUTUMN"}},
		{promotions.StateScheduled, "", nil},
		{"", "AUTUMN2", []string{"AUTUMN2"}},
		{"", "AUT_", nil},
	} {
		listed, err := repo.ListCoupons(ctx, promotions.CouponFilter{State: tc.state, Prefix: tc.prefix, Now: now, Limit: 10})
		if err != nil {
			t.Fatal(err)
		}

		var codes []string
		for _, c := range listed {
			codes = append(codes, c.Code)
		}

		if fmt.Sprint(codes) != fmt.Sprint(tc.want) {
			t.Fatalf("%q %q: %v", tc.state, tc.prefix, codes)
		}
	}
}

func TestAClaimedQuoteReservesItsCouponAndACompletedTripRedeemsIt(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewPricingRepository(pool)

	coupon, err := repo.CreateCoupon(ctx, testCoupon("ONCE", nil))
	if err != nil {
		t.Fatal(err)
	}

	first := quoteWithCoupon(t, repo, testRider, coupon)
	second := quoteWithCoupon(t, repo, testRider, coupon)
	now := time.Now().UTC()

	if _, err := repo.ClaimQuote(ctx, first.ID, testRider, testTrip, now); err != nil {
		t.Fatal(err)
	}

	if uses, _ := repo.RiderRedemptionCount(ctx, coupon.ID, testRider); uses != 1 {
		t.Fatalf("uses %d", uses)
	}

	// Claiming the same quote again for the same trip holds nothing more.
	if _, err := repo.ClaimQuote(ctx, first.ID, testRider, testTrip, now); err != nil {
		t.Fatal(err)
	}

	// The rider's one use is taken: the second quote cannot be claimed, and
	// stays free.
	if _, err := repo.ClaimQuote(ctx, second.ID, testRider, otherTrip, now); !errors.Is(err, pricing.ErrQuoteCouponUnavailable) {
		t.Fatalf("second: %v", err)
	}

	if found, _ := repo.FindQuote(ctx, second.ID); found.ClaimedTripID != "" {
		t.Fatal("a refused claim must not claim")
	}

	// The trip is cancelled: the use is free, the second quote can go.
	if err := repo.ReleaseTripCoupon(ctx, testTrip); err != nil {
		t.Fatal(err)
	}

	if err := repo.ReleaseTripCoupon(ctx, testTrip); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.ClaimQuote(ctx, second.ID, testRider, otherTrip, now); err != nil {
		t.Fatalf("after the release: %v", err)
	}

	// The trip completes: the reservation becomes the use, with the discount.
	if _, err := repo.PersistFare(ctx, pricing.PersistFareInput{
		TripID: otherTrip, RiderID: testRider, QuoteID: second.ID,
		Breakdown: pricing.FareBreakdown{CurrencyCode: "IQD", Total: decimal.NewFromInt(3000)},
		Coupon:    &pricing.AppliedCoupon{CouponID: coupon.ID, DiscountAmount: decimal.NewFromInt(800)},
	}); err != nil {
		t.Fatal(err)
	}

	details, err := repo.FindCouponDetails(ctx, "ONCE")
	if err != nil || details.Coupon.RedemptionCount != 1 || details.RedeemedCount != 1 || !details.DiscountGiven.Equal(decimal.NewFromInt(800)) {
		t.Fatalf("details %+v %v", details, err)
	}

	redemptions, err := repo.ListRedemptions(ctx, coupon.ID, 0, 10)
	if err != nil || len(redemptions) != 2 {
		t.Fatalf("redemptions %+v %v", redemptions, err)
	}

	statuses := map[string]string{}
	for _, r := range redemptions {
		statuses[r.TripID] = r.Status
	}

	if statuses[testTrip] != "released" || statuses[otherTrip] != "redeemed" {
		t.Fatalf("statuses %v", statuses)
	}

	// A released quote frees its use too.
	third := quoteWithCoupon(t, repo, riderN(3), coupon)
	if _, err := repo.ClaimQuote(ctx, third.ID, riderN(3), tripN(3), now); err != nil {
		t.Fatal(err)
	}

	if err := repo.ReleaseQuote(ctx, third.ID, tripN(3)); err != nil {
		t.Fatal(err)
	}

	if uses, _ := repo.RiderRedemptionCount(ctx, coupon.ID, riderN(3)); uses != 0 {
		t.Fatalf("uses after release %d", uses)
	}
}

func TestAFareTakesAUseWhenItsTripReservedNone(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewPricingRepository(pool)

	one := 1
	coupon, err := repo.CreateCoupon(ctx, testCoupon("LAST", &one))
	if err != nil {
		t.Fatal(err)
	}

	fare := func(trip, rider string) error {
		_, err := repo.PersistFare(ctx, pricing.PersistFareInput{
			TripID: trip, RiderID: rider,
			Breakdown: pricing.FareBreakdown{CurrencyCode: "IQD", Total: decimal.NewFromInt(3000)},
			Coupon:    &pricing.AppliedCoupon{CouponID: coupon.ID, DiscountAmount: decimal.NewFromInt(500)},
		})

		return err
	}

	if err := fare(tripN(1), riderN(1)); err != nil {
		t.Fatal(err)
	}

	if err := fare(tripN(2), riderN(2)); !errors.Is(err, pricing.ErrCouponUnavailable) {
		t.Fatalf("the last use was taken: %v", err)
	}

	// Nothing of the refused fare was recorded.
	if _, found, _ := repo.FindFareByTripID(ctx, tripN(2)); found {
		t.Fatal("a refused fare was recorded")
	}
}

func TestTwoTripsNeverShareTheLastUse(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewPricingRepository(pool)

	three := 3
	coupon, err := repo.CreateCoupon(ctx, testCoupon("RUSH", &three))
	if err != nil {
		t.Fatal(err)
	}

	const riders = 12

	quotes := make([]pricing.Quote, riders)
	for i := range quotes {
		quotes[i] = quoteWithCoupon(t, repo, riderN(i), coupon)
	}

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		claimed int
		refused int
	)

	now := time.Now().UTC()

	for i := range quotes {
		wg.Add(1)

		go func(i int) {
			defer wg.Done()

			_, err := repo.ClaimQuote(ctx, quotes[i].ID, riderN(i), tripN(i), now)

			mu.Lock()
			defer mu.Unlock()

			switch {
			case err == nil:
				claimed++
			case errors.Is(err, pricing.ErrQuoteCouponUnavailable):
				refused++
			default:
				t.Errorf("claim %d: %v", i, err)
			}
		}(i)
	}

	wg.Wait()

	details, err := repo.FindCouponDetails(ctx, "RUSH")
	if err != nil {
		t.Fatal(err)
	}

	if claimed != 3 || refused != riders-3 || details.Coupon.RedemptionCount != 3 {
		t.Fatalf("claimed %d, refused %d, held %d", claimed, refused, details.Coupon.RedemptionCount)
	}
}

func TestThePromotionSettings(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewPricingRepository(pool)

	seeded, err := repo.GetPromotionSettings(ctx)
	if err != nil || !seeded.FirstRidePercent.Equal(decimal.NewFromInt(50)) || seeded.LoyaltyEvery != 10 ||
		!seeded.LoyaltyPercent.Equal(decimal.NewFromInt(20)) || seeded.FirstRideMaxAmount != nil {
		t.Fatalf("seeded %+v %v", seeded, err)
	}

	limit := decimal.NewFromInt(3000)

	saved, err := repo.SetPromotionSettings(ctx, pricing.PromotionSettings{
		FirstRidePercent: decimal.NewFromInt(30), FirstRideMaxAmount: &limit,
		LoyaltyEvery: 5, LoyaltyPercent: decimal.NewFromInt(10), UpdatedBy: testStaff,
	})
	if err != nil || saved.LoyaltyEvery != 5 || saved.FirstRideMaxAmount == nil || saved.UpdatedBy != testStaff {
		t.Fatalf("saved %+v %v", saved, err)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM promotion_settings`); err != nil {
		t.Fatal(err)
	}

	if fallback, err := repo.GetPromotionSettings(ctx); err != nil || fallback.LoyaltyEvery != 10 {
		t.Fatalf("without a row: %+v %v", fallback, err)
	}
}

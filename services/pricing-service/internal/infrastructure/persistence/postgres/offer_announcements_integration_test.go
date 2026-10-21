package postgres

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/pricing"
)

func offerEvents(t *testing.T, repo *PricingRepository, eventType string) []map[string]any {
	t.Helper()

	rows, err := repo.pool.Query(context.Background(),
		`SELECT payload FROM outbox_events WHERE event_type = $1 ORDER BY id`, eventType)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var out []map[string]any

	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}

		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatal(err)
		}

		out = append(out, payload)
	}

	return out
}

func TestTheRiderIsToldWhenTheirNextTripIsTheLoyaltyOne(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewPricingRepository(pool)

	if _, err := pool.Exec(ctx, `UPDATE promotion_settings SET loyalty_every = 3, loyalty_percent = 25, loyalty_max_amount = 5000`); err != nil {
		t.Fatal(err)
	}

	for n := 1; n <= 5; n++ {
		if _, err := repo.PersistFare(ctx, pricing.PersistFareInput{
			TripID: tripN(100 + n), RiderID: riderN(7),
			Breakdown: pricing.FareBreakdown{CurrencyCode: "IQD", Total: decimal.NewFromInt(3000)},
		}); err != nil {
			t.Fatal(err)
		}
	}

	// A fee is not a completed trip.
	if _, err := repo.PersistFare(ctx, pricing.PersistFareInput{
		TripID: tripN(200), RiderID: riderN(7), Kind: pricing.FareKindCancellation,
		Breakdown: pricing.FareBreakdown{CurrencyCode: "IQD", Total: decimal.NewFromInt(1000)},
	}); err != nil {
		t.Fatal(err)
	}

	events := offerEvents(t, repo, eventLoyaltyDue)
	// Five trips: after the 2nd (the 3rd is the discount's) and the 5th (the 6th).
	if len(events) != 2 || events[0]["trip_number"] != float64(3) || events[1]["trip_number"] != float64(6) {
		t.Fatalf("events %v", events)
	}

	if e := events[0]; e["rider_id"] != riderN(7) || e["percent"] != "25" || e["max_amount"] != "5000" {
		t.Fatalf("payload %v", e)
	}

	if _, err := pool.Exec(ctx, `UPDATE promotion_settings SET loyalty_every = 0`); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.PersistFare(ctx, pricing.PersistFareInput{
		TripID: tripN(300), RiderID: riderN(8),
		Breakdown: pricing.FareBreakdown{CurrencyCode: "IQD", Total: decimal.NewFromInt(3000)},
	}); err != nil {
		t.Fatal(err)
	}

	if events := offerEvents(t, repo, eventLoyaltyDue); len(events) != 2 {
		t.Fatalf("loyalty switched off: %v", events)
	}
}

func TestARunningFeaturedCouponIsAnnouncedOnce(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewPricingRepository(pool)
	now := time.Now().UTC()

	coupons := map[string]func(c *pricing.Coupon){
		"FEATURED": func(c *pricing.Coupon) { c.Featured = true },
		"PLAIN":    func(*pricing.Coupon) {},
		"NEWONLY":  func(c *pricing.Coupon) { c.Featured, c.NewRidersOnly = true, true },
		"LATER":    func(c *pricing.Coupon) { c.Featured, c.ValidFrom = true, now.Add(time.Hour) },
		"OFF":      func(c *pricing.Coupon) { c.Featured, c.Active = true, false },
	}

	for code, change := range coupons {
		coupon := testCoupon(code, nil)
		change(&coupon)

		if _, err := repo.CreateCoupon(ctx, coupon); err != nil {
			t.Fatal(err)
		}
	}

	if n, err := repo.AnnounceFeaturedCoupons(ctx, now); err != nil || n != 1 {
		t.Fatalf("announced %d, %v", n, err)
	}

	if n, _ := repo.AnnounceFeaturedCoupons(ctx, now.Add(time.Minute)); n != 0 {
		t.Fatalf("announced again: %d", n)
	}

	events := offerEvents(t, repo, eventFeaturedOffer)
	if len(events) != 1 || events[0]["coupon_code"] != "FEATURED" || events[0]["discount_value"] != "20" || events[0]["description"] != "Autumn" {
		t.Fatalf("events %v", events)
	}

	// The one starting later is announced once it starts.
	if n, _ := repo.AnnounceFeaturedCoupons(ctx, now.Add(2*time.Hour)); n != 1 {
		t.Fatalf("at its start: %d", n)
	}
}

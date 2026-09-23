package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/pricing"
)

func TestAFeeIsNotACompletedTrip(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewPricingRepository(pool)

	base, err := repo.GetActiveConfig(ctx, pricing.Scope{})
	if err != nil {
		t.Fatal(err)
	}

	byID, err := repo.GetConfigByID(ctx, base.ID)
	if err != nil || byID.ID != base.ID {
		t.Fatalf("by id: %+v %v", byID, err)
	}

	if _, err := repo.GetConfigByID(ctx, testTrip); !errors.Is(err, pricing.ErrNoActiveConfig) {
		t.Fatalf("unknown card: %v", err)
	}

	fee, err := repo.PersistFare(ctx, pricing.PersistFareInput{
		TripID: testTrip, RiderID: testRider, Kind: pricing.FareKindNoShow, ConfigID: base.ID,
		Breakdown: pricing.FareBreakdown{CurrencyCode: "IQD", Total: decimal.NewFromInt(2000)},
		Coupon:    &pricing.AppliedCoupon{CouponID: testZone, DiscountAmount: decimal.NewFromInt(1)},
	})
	if err != nil || fee.Kind != pricing.FareKindNoShow || fee.ConfigID != base.ID {
		t.Fatalf("fee %+v %v", fee, err)
	}

	if count, _ := repo.GetRiderCompletedTripCount(ctx, testRider); count != 0 {
		t.Fatalf("a fee counted as a completed trip: %d", count)
	}

	var payload string
	if err := pool.QueryRow(ctx, `SELECT payload::text FROM outbox_events WHERE aggregate_id = $1`, fee.ID).Scan(&payload); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(payload, `"kind": "no_show"`) {
		t.Fatalf("payload %s", payload)
	}

	trip, err := repo.PersistFare(ctx, pricing.PersistFareInput{
		TripID: otherTrip, RiderID: testRider,
		Breakdown: pricing.FareBreakdown{CurrencyCode: "IQD", WaitingMinutes: 4, WaitingFare: decimal.NewFromInt(400), Total: decimal.NewFromInt(5000)},
	})
	if err != nil || trip.Kind != pricing.FareKindTrip || trip.Breakdown.WaitingMinutes != 4 || !trip.Breakdown.WaitingFare.Equal(decimal.NewFromInt(400)) {
		t.Fatalf("trip fare %+v %v", trip, err)
	}

	if count, _ := repo.GetRiderCompletedTripCount(ctx, testRider); count != 1 {
		t.Fatalf("completed trips %d", count)
	}
}

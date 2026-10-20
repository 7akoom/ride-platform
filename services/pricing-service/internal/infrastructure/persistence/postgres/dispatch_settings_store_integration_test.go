package postgres

import (
	"context"
	"testing"

	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/dispatchsettings"
)

func TestTheDispatchSettings(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewPricingRepository(pool)

	seeded, err := repo.GetDispatchSettings(ctx)
	if err != nil || !seeded.NextTripsEnabled || seeded.NextTripRadiusMeters != 3000 || seeded.UpdatedBy != "" {
		t.Fatalf("seeded %+v %v", seeded, err)
	}

	saved, err := repo.SetDispatchSettings(ctx, dispatchSettings(false, 1500))
	if err != nil || saved.NextTripsEnabled || saved.NextTripRadiusMeters != 1500 || saved.UpdatedBy != testStaff {
		t.Fatalf("saved %+v %v", saved, err)
	}

	if _, err := repo.SetDispatchSettings(ctx, dispatchSettings(true, 20000)); err == nil {
		t.Fatal("the table refuses a radius over 10 km")
	}

	if _, err := pool.Exec(ctx, `DELETE FROM dispatch_settings`); err != nil {
		t.Fatal(err)
	}

	if fallback, err := repo.GetDispatchSettings(ctx); err != nil || !fallback.NextTripsEnabled || fallback.NextTripRadiusMeters != 3000 {
		t.Fatalf("without a row: %+v %v", fallback, err)
	}
}

func dispatchSettings(enabled bool, radius int) dispatchsettings.Settings {
	return dispatchsettings.Settings{NextTripsEnabled: enabled, NextTripRadiusMeters: radius, UpdatedBy: testStaff}
}

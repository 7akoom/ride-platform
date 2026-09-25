package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/pricing"
)

func TestAQuoteKeepsItsStopsInOrder(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewPricingRepository(pool)
	now := time.Now().UTC()

	withStops := quoteFor(testRider, now.Add(5*time.Minute))
	withStops.Stops = []pricing.Point{{Latitude: 36.195, Longitude: 44.015}, {Latitude: 36.198, Longitude: 44.018}}

	saved, err := repo.SaveQuotes(ctx, []pricing.Quote{withStops, quoteFor(testRider, now.Add(5*time.Minute))})
	if err != nil {
		t.Fatal(err)
	}

	if len(saved[0].Stops) != 2 || saved[0].Stops[1] != withStops.Stops[1] || len(saved[1].Stops) != 0 {
		t.Fatalf("saved %v and %v", saved[0].Stops, saved[1].Stops)
	}

	claimed, err := repo.ClaimQuote(ctx, saved[0].ID, testRider, testTrip, now)
	if err != nil || len(claimed.Stops) != 2 || claimed.Stops[0] != withStops.Stops[0] {
		t.Fatalf("claimed %+v %v", claimed.Stops, err)
	}

	// The table refuses a third stop even if a caller forgot the check.
	three := quoteFor(testRider, now.Add(5*time.Minute))
	three.Stops = append(append([]pricing.Point{}, withStops.Stops...), withStops.Stops[0])

	if _, err := repo.SaveQuotes(ctx, []pricing.Quote{three}); err == nil {
		t.Fatal("a quote with three stops was stored")
	}
}

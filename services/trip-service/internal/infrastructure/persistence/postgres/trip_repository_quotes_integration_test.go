package postgres

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

func TestATripKeepsItsQuote(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewTripRepository(pool)
	quote := uuid.NewString()

	input := trip.CreateInput{
		ID: uuid.NewString(), RiderID: uuid.NewString(), VehicleClass: "comfort",
		Pickup:  trip.Coordinates{Latitude: 36.1, Longitude: 44.1},
		Dropoff: trip.Coordinates{Latitude: 36.2, Longitude: 44.2},
		QuoteID: quote, QuotedFare: "7250.5", CurrencyCode: "IQD",
	}

	created, err := repo.Create(ctx, input)
	if err != nil {
		t.Fatal(err)
	}

	if created.QuoteID != quote || created.QuotedFare != "7250.5" || created.CurrencyCode != "IQD" || created.VehicleClass != "comfort" {
		t.Fatalf("created %+v", created)
	}

	found, err := repo.FindByID(ctx, created.ID)
	if err != nil || found.QuotedFare != "7250.5" {
		t.Fatalf("found %+v %v", found, err)
	}

	// A quote pays for one trip.
	input.ID, input.RiderID = uuid.NewString(), uuid.NewString()
	if _, err := repo.Create(ctx, input); err == nil {
		t.Fatal("a second trip with the same quote was created")
	}

	plain, err := repo.Create(ctx, trip.CreateInput{
		ID: uuid.NewString(), RiderID: uuid.NewString(),
		Pickup:  trip.Coordinates{Latitude: 36.1, Longitude: 44.1},
		Dropoff: trip.Coordinates{Latitude: 36.2, Longitude: 44.2},
	})
	if err != nil || plain.QuoteID != "" || plain.QuotedFare != "" || plain.CurrencyCode != "" {
		t.Fatalf("a trip without a quote: %+v %v", plain, err)
	}
}

func TestTrimDecimal(t *testing.T) {
	for in, want := range map[string]string{"4500.0000": "4500", "4500.5000": "4500.5", "0.2500": "0.25", "12": "12", "": ""} {
		if got := trimDecimal(in); got != want {
			t.Fatalf("%q: got %q, want %q", in, got, want)
		}
	}
}

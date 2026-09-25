package pricing

import (
	"context"
	"errors"
	"testing"
)

var twoStops = []Point{{Latitude: 36.195, Longitude: 44.015}, {Latitude: 36.198, Longitude: 44.018}}

func TestATripIsPricedThroughItsStopsInOrder(t *testing.T) {
	h := newHarness()

	input := quoteInput()
	input.Stops = twoStops

	got, err := h.service().QuoteTrip(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}

	if len(h.routing.via) != 2 || h.routing.via[0] != twoStops[0] || h.routing.via[1] != twoStops[1] {
		t.Fatalf("routed via %v", h.routing.via)
	}

	for _, quote := range got.Quotes {
		if len(quote.Stops) != 2 || quote.Stops[1] != twoStops[1] {
			t.Fatalf("%s kept %v", quote.VehicleClass, quote.Stops)
		}
	}

	estimate := validEstimateInput()
	estimate.Stops = twoStops[:1]
	h.routing.via = nil

	if _, err := h.service().EstimateFare(context.Background(), estimate); err != nil || len(h.routing.via) != 1 {
		t.Fatalf("estimate: %v via %v", err, h.routing.via)
	}
}

func TestAMeteredTripIsPricedThroughItsStops(t *testing.T) {
	h := newHarness()

	if _, err := h.service().CalculateFare(context.Background(), CalculateFareInput{
		TripID: tripA, RiderID: riderA, PickupLat: 36.19, PickupLng: 44.01, DropoffLat: 36.2, DropoffLng: 44.02,
		Stops: twoStops,
	}); err != nil {
		t.Fatal(err)
	}

	if len(h.routing.via) != 2 {
		t.Fatalf("routed via %v", h.routing.via)
	}
}

func TestStopsAreCheckedBeforeAnythingIsPriced(t *testing.T) {
	for name, stops := range map[string][]Point{
		"three":          append(append([]Point{}, twoStops...), Point{Latitude: 36.2, Longitude: 44.0}),
		"a bad latitude": {{Latitude: 95, Longitude: 44}},
	} {
		h := newHarness()
		input := quoteInput()
		input.Stops = stops

		if _, err := h.service().QuoteTrip(context.Background(), input); err == nil || h.repo.savedCount != 0 {
			t.Errorf("%s: %v", name, err)
		}
	}

	h := newHarness()
	input := quoteInput()
	input.Stops = append(append([]Point{}, twoStops...), twoStops[0])

	if _, err := h.service().QuoteTrip(context.Background(), input); !errors.Is(err, ErrTooManyStops) {
		t.Fatalf("got %v", err)
	}
}

func TestWithoutOSRMAStopsDetourIsEstimatedLegByLeg(t *testing.T) {
	config := Config{DistanceCorrectionFactor: 1, AverageSpeedKmh: 30}

	direct := fallbackRoute(config, 0, 0, 0, 1)
	// Out to a point north of the line and back down: two legs, each longer
	// than half the direct line.
	detour := fallbackRoute(config, 0, 0, 0, 1, Point{Latitude: 0.5, Longitude: 0.5})

	if detour.DistanceKm <= direct.DistanceKm || detour.DurationMinutes <= direct.DurationMinutes || !detour.Estimated {
		t.Fatalf("direct %+v, detour %+v", direct, detour)
	}
}

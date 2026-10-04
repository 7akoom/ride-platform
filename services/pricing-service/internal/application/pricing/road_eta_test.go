package pricing

import (
	"context"
	"errors"
	"testing"
)

type fakeTravelTimer struct {
	times []TravelTime
	err   error
	asked int
}

func (f *fakeTravelTimer) TravelTimes(_ context.Context, origins []Point, _ Point) ([]TravelTime, error) {
	f.asked = len(origins)

	return f.times, f.err
}

func TestQuoteETAIsTheFastestDriverOfTheClassByRoad(t *testing.T) {
	h := newHarness()
	h.drivers.drivers = []NearbyDriver{
		{DriverID: "nearest-economy", VehicleClass: VehicleClassEconomy, Location: Point{Latitude: 36.191, Longitude: 44.01}},
		{DriverID: "comfort", VehicleClass: VehicleClassComfort, Location: Point{Latitude: 36.192, Longitude: 44.01}},
		{DriverID: "no-road", VehicleClass: VehicleClassEconomy, Location: Point{Latitude: 36.193, Longitude: 44.01}},
		{DriverID: "fast-economy", VehicleClass: VehicleClassEconomy, Location: Point{Latitude: 36.194, Longitude: 44.01}},
	}
	timer := &fakeTravelTimer{times: []TravelTime{
		{Reachable: true, DurationSeconds: 900},
		{Reachable: true, DurationSeconds: 61},
		{},
		{Reachable: true, DurationSeconds: 170},
	}}

	got, err := h.service(WithTravelTimes(timer)).QuoteTrip(context.Background(), quoteInput())
	if err != nil {
		t.Fatal(err)
	}

	eta := map[string]int{}
	for _, q := range got.Quotes {
		eta[q.VehicleClass] = q.PickupETAMinutes
	}

	// 170 s is 3 minutes (rounded up), 61 s is 2; one call for all drivers.
	if eta[VehicleClassEconomy] != 3 || eta[VehicleClassComfort] != 2 || timer.asked != 4 {
		t.Fatalf("eta %v, asked %d", eta, timer.asked)
	}
}

func TestQuoteETAFallsBackToTheRouteWhenTravelTimesFail(t *testing.T) {
	h := newHarness()
	timer := &fakeTravelTimer{err: errors.New("unavailable")}

	got, err := h.service(WithTravelTimes(timer)).QuoteTrip(context.Background(), quoteInput())
	if err != nil {
		t.Fatal(err)
	}

	// The nearest economy driver's route, as without travel times (20 minutes).
	for _, q := range got.Quotes {
		if q.VehicleClass == VehicleClassEconomy && q.PickupETAMinutes != 20 {
			t.Fatalf("eta %d", q.PickupETAMinutes)
		}
	}
}

func TestQuoteETAFallsBackWhenNoDriverOfTheClassHasARoad(t *testing.T) {
	h := newHarness()
	h.drivers.drivers = someDrivers(2)
	timer := &fakeTravelTimer{times: []TravelTime{{}, {}}}

	got, err := h.service(WithTravelTimes(timer)).QuoteTrip(context.Background(), quoteInput())
	if err != nil {
		t.Fatal(err)
	}

	for _, q := range got.Quotes {
		if q.VehicleClass == VehicleClassEconomy && (!q.DriversAvailable || q.PickupETAMinutes != 20) {
			t.Fatalf("economy %v %d", q.DriversAvailable, q.PickupETAMinutes)
		}
	}
}

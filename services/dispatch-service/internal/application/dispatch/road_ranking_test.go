package dispatch_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/dispatch-service/internal/application/dispatch"
)

type fakeTravelTimes struct {
	times       map[string]dispatch.TravelTime // by origin, "lat,lng"
	err         error
	destination dispatch.Point
	asked       int
}

func key(p dispatch.Point) string {
	return fmt.Sprintf("%.6f,%.6f", p.Latitude, p.Longitude)
}

func (f *fakeTravelTimes) TravelTimes(_ context.Context, origins []dispatch.Point, destination dispatch.Point) ([]dispatch.TravelTime, error) {
	f.destination = destination
	f.asked = len(origins)

	if f.err != nil {
		return nil, f.err
	}

	out := make([]dispatch.TravelTime, len(origins))
	for i, origin := range origins {
		out[i] = f.times[key(origin)]
	}

	return out, nil
}

func at(id string, meters, lat, lng float64) dispatch.NearbyDriver {
	return dispatch.NearbyDriver{DriverID: id, DistanceMeters: meters, Latitude: lat, Longitude: lng}
}

func roadHarness() (*harness, *fakeTravelTimes) {
	h := newHarness()

	// Straight-line order: across-the-river (300 m), unknown (500 m), down-the-road (900 m).
	h.location.candidates = []dispatch.NearbyDriver{
		at("across-the-river", 300, 36.191, 44.011),
		at("unknown", 500, 36.192, 44.012),
		at("down-the-road", 900, 36.193, 44.013),
	}

	for _, id := range []string{"across-the-river", "unknown", "down-the-road"} {
		h.driver.drivers[id] = eligibleDriver(id)
		h.wallet.standingFor[id] = goodStanding()
	}

	travel := &fakeTravelTimes{times: map[string]dispatch.TravelTime{
		key(dispatch.Point{Latitude: 36.191, Longitude: 44.011}): {Reachable: true, DurationSeconds: 840},
		key(dispatch.Point{Latitude: 36.193, Longitude: 44.013}): {Reachable: true, DurationSeconds: 150},
	}}

	return h, travel
}

func TestRoadRanking_TheFastestByRoadIsAssignedNotTheNearest(t *testing.T) {
	h, travel := roadHarness()

	result, err := dispatch.NewService(h.trip, h.location, h.driver, h.wallet,
		dispatch.WithRoadRanking(travel, 20*time.Minute)).DispatchTrip(context.Background(), "trip-1", 0)
	if err != nil {
		t.Fatal(err)
	}

	if result.DriverID != "down-the-road" || result.PickupETASeconds != 150 || result.DistanceMeters != 900 {
		t.Fatalf("result %+v", result)
	}

	if travel.asked != 3 || travel.destination != (dispatch.Point{Latitude: 36.19, Longitude: 44.01}) {
		t.Fatalf("asked %d origins to %+v", travel.asked, travel.destination)
	}
}

func TestRoadRanking_KnownTimesComeBeforeUnknownOnes(t *testing.T) {
	h, travel := roadHarness()
	h.trip.acceptErr = map[string]error{"down-the-road": errors.New("taken")}

	result, err := dispatch.NewService(h.trip, h.location, h.driver, h.wallet,
		dispatch.WithRoadRanking(travel, 0)).DispatchTrip(context.Background(), "trip-1", 0)
	if err != nil {
		t.Fatal(err)
	}

	if result.DriverID != "across-the-river" || result.PickupETASeconds != 840 {
		t.Fatalf("result %+v", result)
	}
}

func TestRoadRanking_DriversTooFarByRoadAreLeftOut(t *testing.T) {
	h, travel := roadHarness()
	h.trip.acceptErr = map[string]error{"down-the-road": errors.New("taken")}

	// across-the-river is 14 minutes away; with a 10 minute limit only the
	// driver whose time is unknown is left to try.
	result, err := dispatch.NewService(h.trip, h.location, h.driver, h.wallet,
		dispatch.WithRoadRanking(travel, 10*time.Minute)).DispatchTrip(context.Background(), "trip-1", 0)
	if err != nil {
		t.Fatal(err)
	}

	if result.DriverID != "unknown" || result.PickupETASeconds != 0 {
		t.Fatalf("result %+v", result)
	}

	h.trip.acceptErr["unknown"] = errors.New("taken")

	_, err = dispatch.NewService(h.trip, h.location, h.driver, h.wallet,
		dispatch.WithRoadRanking(travel, 10*time.Minute)).DispatchTrip(context.Background(), "trip-1", 0)
	if !errors.Is(err, dispatch.ErrNoDriversAvailable) {
		t.Fatalf("everyone too far or taken: %v", err)
	}
}

func TestRoadRanking_WithoutTheMapServiceTheStraightLineOrderIsUsed(t *testing.T) {
	h, travel := roadHarness()
	travel.err = errors.New("unavailable")

	result, err := dispatch.NewService(h.trip, h.location, h.driver, h.wallet,
		dispatch.WithRoadRanking(travel, 10*time.Minute)).DispatchTrip(context.Background(), "trip-1", 0)
	if err != nil {
		t.Fatal(err)
	}

	if result.DriverID != "across-the-river" || result.PickupETASeconds != 0 {
		t.Fatalf("result %+v", result)
	}
}

func TestRoadRanking_RefusesBadOptions(t *testing.T) {
	h, travel := roadHarness()

	for i, option := range []func() dispatch.Option{
		func() dispatch.Option { return dispatch.WithRoadRanking(nil, 0) },
		func() dispatch.Option { return dispatch.WithRoadRanking(travel, -time.Second) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("case %d: expected a panic", i)
				}
			}()

			dispatch.NewService(h.trip, h.location, h.driver, h.wallet, option())
		}()
	}
}

package routing

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/location-service/internal/application/maps"
)

var (
	mall    = maps.Coordinates{Latitude: 36.2001, Longitude: 44.0201}
	desert  = maps.Coordinates{Latitude: 36.4, Longitude: 43.5}
	citadel = maps.Coordinates{Latitude: 36.1912, Longitude: 44.0094}
)

func TestTravelTimesAreReadFromOSRMsTable(t *testing.T) {
	var seen string

	// The second origin has no way by road (null); the third snapped 4 km to a road.
	body := `{"code":"Ok",
		"durations":[[300.5],[null],[90]],
		"distances":[[2100.2],[null],[700]],
		"sources":[{"distance":3.1},{"distance":12},{"distance":4000}],
		"destinations":[{"distance":5}]}`

	client := NewOSRMClient(osrmServer(t, http.StatusOK, body, &seen).URL, time.Second)

	times, err := client.TravelTimes(context.Background(), []maps.Coordinates{erbil, mall, desert}, citadel)
	if err != nil {
		t.Fatal(err)
	}

	want := []maps.TravelTime{
		{Reachable: true, DurationSeconds: 300.5, DistanceMeters: 2100.2},
		{},
		{},
	}

	for i := range want {
		if times[i] != want[i] {
			t.Errorf("origin %d: %+v, want %+v", i, times[i], want[i])
		}
	}

	for _, part := range []string{
		"/table/v1/driving/44.009200,36.191100;44.020100,36.200100;43.500000,36.400000;44.009400,36.191200",
		"sources=0;1;2",
		"destinations=3",
		"annotations=duration,distance",
	} {
		if !strings.Contains(seen, part) {
			t.Errorf("request %q lacks %q", seen, part)
		}
	}

	if strings.Contains(seen, "radiuses") {
		t.Errorf("a snapping radius makes one bad origin fail the whole table: %q", seen)
	}
}

func TestTravelTimesToAPointFarFromRoadsOrWithOSRMDown(t *testing.T) {
	far := `{"code":"Ok","durations":[[10]],"distances":[[10]],"sources":[{"distance":1}],"destinations":[{"distance":5000}]}`

	_, err := NewOSRMClient(osrmServer(t, http.StatusOK, far, nil).URL, time.Second).
		TravelTimes(context.Background(), []maps.Coordinates{erbil}, desert)
	if !errors.Is(err, maps.ErrNotNearRoad) {
		t.Fatalf("destination far from roads: %v", err)
	}

	_, err = NewOSRMClient(osrmServer(t, http.StatusBadGateway, "<html>", nil).URL, time.Second).
		TravelTimes(context.Background(), []maps.Coordinates{erbil}, citadel)
	if !errors.Is(err, maps.ErrUnavailable) {
		t.Fatalf("OSRM down: %v", err)
	}

	short := `{"code":"Ok","durations":[[10]],"sources":[{"distance":1}],"destinations":[{"distance":1}]}`

	_, err = NewOSRMClient(osrmServer(t, http.StatusOK, short, nil).URL, time.Second).
		TravelTimes(context.Background(), []maps.Coordinates{erbil, mall}, citadel)
	if !errors.Is(err, maps.ErrUnavailable) {
		t.Fatalf("a table of the wrong size: %v", err)
	}
}

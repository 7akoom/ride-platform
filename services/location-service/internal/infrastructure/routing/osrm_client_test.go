package routing

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/location-service/internal/application/maps"
)

func osrmServer(t *testing.T, status int, body string, seen *string) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if seen != nil {
			*seen = r.URL.String()
		}

		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	return server
}

var (
	erbil   = maps.Coordinates{Latitude: 36.1911, Longitude: 44.0092}
	airport = maps.Coordinates{Latitude: 36.2367, Longitude: 43.9631}
)

const okRoute = `{"code":"Ok","routes":[{"distance":8693.4,"duration":806.1,"geometry":"_p~iF~ps|U_ulLnnqC_mqNvxq` + "`" + `@","legs":[]}],"waypoints":[]}`

func TestARouteIsReadFromOSRMsAnswer(t *testing.T) {
	var seen string

	client := NewOSRMClient(osrmServer(t, http.StatusOK, okRoute, &seen).URL, time.Second)

	route, err := client.Route(context.Background(), maps.RouteQuery{From: erbil, To: airport})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if route.DistanceMeters != 8693.4 || route.DurationSeconds != 806.1 || route.Polyline != "_p~iF~ps|U_ulLnnqC_mqNvxq`@" {
		t.Errorf("unexpected route: %+v", route)
	}

	// Longitude first, six decimals, the whole geometry, and a snapping radius so a
	// point far from any road is refused instead of moved.
	for _, want := range []string{
		"/route/v1/driving/44.009200,36.191100;43.963100,36.236700",
		"overview=full",
		"geometries=polyline",
		"steps=false",
		"radiuses=1000;1000",
	} {
		if !strings.Contains(seen, want) {
			t.Errorf("the request %q lacks %q", seen, want)
		}
	}
}

func TestOSRMsVerdictsBecomeWhatTheAppsCanActOn(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		is     error
		not    error
	}{
		{"no way between the points", http.StatusOK, `{"code":"NoRoute","message":"Impossible route between points"}`, maps.ErrNoRoute, maps.ErrUnavailable},
		{"a point that is not near a road", http.StatusBadRequest, `{"code":"NoSegment","message":"Could not find a matching segment for coordinate 0"}`, maps.ErrNotNearRoad, maps.ErrUnavailable},
		{"a query OSRM refuses", http.StatusBadRequest, `{"code":"InvalidQuery","message":"Query string malformed"}`, nil, maps.ErrUnavailable},
		{"an answer that is not JSON", http.StatusBadGateway, `<html>bad gateway</html>`, maps.ErrUnavailable, nil},
		{"an empty answer", http.StatusOK, ``, maps.ErrUnavailable, nil},
		{"Ok without a route", http.StatusOK, `{"code":"Ok","routes":[]}`, maps.ErrUnavailable, nil},
		{"a server error with a body that has no code", http.StatusInternalServerError, `{"error":"boom"}`, maps.ErrUnavailable, nil},
	}

	for _, tc := range cases {
		client := NewOSRMClient(osrmServer(t, tc.status, tc.body, nil).URL, time.Second)

		_, err := client.Route(context.Background(), maps.RouteQuery{From: erbil, To: airport})
		if err == nil {
			t.Errorf("%s: expected an error", tc.name)

			continue
		}

		if tc.is != nil && !errors.Is(err, tc.is) {
			t.Errorf("%s: expected %v, got %v", tc.name, tc.is, err)
		}

		if tc.not != nil && errors.Is(err, tc.not) {
			t.Errorf("%s: must not be %v: %v", tc.name, tc.not, err)
		}
	}
}

func TestAnOSRMThatCannotBeReachedIsUnavailable(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	url := server.URL
	server.Close() // nothing listens any more

	if _, err := NewOSRMClient(url, time.Second).Route(context.Background(), maps.RouteQuery{From: erbil, To: airport}); !errors.Is(err, maps.ErrUnavailable) {
		t.Errorf("expected ErrUnavailable, got %v", err)
	}
}

func TestASlowOSRMTimesOutAsUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer server.Close()

	if _, err := NewOSRMClient(server.URL, 50*time.Millisecond).Route(context.Background(), maps.RouteQuery{From: erbil, To: airport}); !errors.Is(err, maps.ErrUnavailable) {
		t.Errorf("expected ErrUnavailable, got %v", err)
	}
}

func TestACancelledRequestIsNotSentOn(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := NewOSRMClient(osrmServer(t, http.StatusOK, okRoute, nil).URL, time.Second).Route(ctx, maps.RouteQuery{From: erbil, To: airport}); err == nil {
		t.Error("a cancelled context must fail the call")
	}
}

func TestATrailingSlashInTheBaseURLDoesNotBreakThePath(t *testing.T) {
	var seen string

	client := NewOSRMClient(osrmServer(t, http.StatusOK, okRoute, &seen).URL+"/", time.Second)
	if _, err := client.Route(context.Background(), maps.RouteQuery{From: erbil, To: airport}); err != nil {
		t.Fatal(err)
	}

	if strings.Contains(seen, "//route") {
		t.Errorf("double slash in %q", seen)
	}
}

func TestViaPointsGoBetweenTheEndsInOrderEachWithASnapRadius(t *testing.T) {
	var seen string

	client := NewOSRMClient(osrmServer(t, http.StatusOK, okRoute, &seen).URL, time.Second)
	stop := maps.Coordinates{Latitude: 36.2, Longitude: 44.0}

	if _, err := client.Route(context.Background(), maps.RouteQuery{From: erbil, To: airport, Via: []maps.Coordinates{stop}}); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(seen, "/route/v1/driving/44.009200,36.191100;44.000000,36.200000;43.963100,36.236700?") ||
		!strings.Contains(seen, "radiuses=1000;1000;1000") {
		t.Errorf("asked %q", seen)
	}
}

// Two steps of a real Erbil answer, trimmed: leave west on 60 Meter Street, then
// turn right onto a street with a number but no name, then arrive.
const stepsRoute = `{"code":"Ok","routes":[{"distance":1200,"duration":120,"geometry":"abc","legs":[` +
	`{"steps":[` +
	`{"name":"شارع 60 متري","ref":"","distance":800,"duration":80,"maneuver":{"type":"depart","location":[44.0092,36.1911],"bearing_before":0,"bearing_after":268}},` +
	`{"name":"","ref":"Road 2","distance":400,"duration":40,"maneuver":{"type":"turn","modifier":"right","location":[44.0001,36.1912],"bearing_before":268,"bearing_after":358}}` +
	`]},` +
	`{"steps":[` +
	`{"name":"","distance":0,"duration":0,"maneuver":{"type":"roundabout","modifier":"right","exit":2,"location":[43.99,36.2],"bearing_before":358,"bearing_after":10}},` +
	`{"name":"","distance":0,"duration":0,"maneuver":{"type":"arrive","location":[43.9631,36.2367],"bearing_before":10,"bearing_after":0}}` +
	`]}]}],"waypoints":[]}`

func TestStepsAreAskedForAndReadLegByLeg(t *testing.T) {
	var seen string

	client := NewOSRMClient(osrmServer(t, http.StatusOK, stepsRoute, &seen).URL, time.Second)

	route, err := client.Route(context.Background(), maps.RouteQuery{From: erbil, To: airport, Steps: true})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(seen, "steps=true") {
		t.Errorf("asked %q", seen)
	}

	if len(route.Steps) != 4 {
		t.Fatalf("want 4 steps, got %+v", route.Steps)
	}

	depart, turn, roundabout, arrive := route.Steps[0], route.Steps[1], route.Steps[2], route.Steps[3]

	if depart.Maneuver.Type != "depart" || depart.Name != "شارع 60 متري" || depart.DistanceMeters != 800 || depart.Maneuver.BearingAfter != 268 || depart.Leg != 0 {
		t.Errorf("depart %+v", depart)
	}

	if turn.Maneuver.Type != "turn" || turn.Maneuver.Modifier != "right" || turn.Ref != "Road 2" ||
		turn.Maneuver.Location != (maps.Coordinates{Latitude: 36.1912, Longitude: 44.0001}) {
		t.Errorf("turn %+v", turn)
	}

	if roundabout.Maneuver.Exit != 2 || roundabout.Leg != 1 || arrive.Maneuver.Type != "arrive" || arrive.Leg != 1 {
		t.Errorf("second leg %+v, %+v", roundabout, arrive)
	}
}

func TestWithoutAskingThereAreNoSteps(t *testing.T) {
	client := NewOSRMClient(osrmServer(t, http.StatusOK, stepsRoute, nil).URL, time.Second)

	route, err := client.Route(context.Background(), maps.RouteQuery{From: erbil, To: airport})
	if err != nil || route.Steps != nil {
		t.Fatalf("got %+v, %v", route.Steps, err)
	}
}

func TestAHeadingConstrainsOnlyTheStart(t *testing.T) {
	var seen string

	heading := 359.6
	client := NewOSRMClient(osrmServer(t, http.StatusOK, okRoute, &seen).URL, time.Second)
	stop := maps.Coordinates{Latitude: 36.2, Longitude: 44.0}

	if _, err := client.Route(context.Background(), maps.RouteQuery{From: erbil, To: airport, Via: []maps.Coordinates{stop}, Heading: &heading}); err != nil {
		t.Fatal(err)
	}

	// 359.6 rounds to 360, which OSRM wants as 0.
	if !strings.Contains(seen, "bearings=0,60;;") || strings.Contains(seen, "bearings=0,60;;;") {
		t.Errorf("asked %q", seen)
	}
}

func TestAHeadingNoRoadFollowsIsDroppedRatherThanFailing(t *testing.T) {
	var asked []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.String())

		if strings.Contains(r.URL.RawQuery, "bearings=") {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"code":"NoSegment","message":"Could not find a matching segment"}`))

			return
		}

		_, _ = w.Write([]byte(okRoute))
	}))
	t.Cleanup(server.Close)

	heading := 90.0

	route, err := NewOSRMClient(server.URL, time.Second).Route(context.Background(), maps.RouteQuery{From: erbil, To: airport, Heading: &heading})
	if err != nil || route.DistanceMeters != 8693.4 {
		t.Fatalf("got %+v, %v", route, err)
	}

	if len(asked) != 2 || strings.Contains(asked[1], "bearings=") {
		t.Errorf("asked %v", asked)
	}
}

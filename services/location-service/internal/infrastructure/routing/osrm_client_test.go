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

	route, err := client.Route(context.Background(), erbil, airport)
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

		_, err := client.Route(context.Background(), erbil, airport)
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

	if _, err := NewOSRMClient(url, time.Second).Route(context.Background(), erbil, airport); !errors.Is(err, maps.ErrUnavailable) {
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

	if _, err := NewOSRMClient(server.URL, 50*time.Millisecond).Route(context.Background(), erbil, airport); !errors.Is(err, maps.ErrUnavailable) {
		t.Errorf("expected ErrUnavailable, got %v", err)
	}
}

func TestACancelledRequestIsNotSentOn(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := NewOSRMClient(osrmServer(t, http.StatusOK, okRoute, nil).URL, time.Second).Route(ctx, erbil, airport); err == nil {
		t.Error("a cancelled context must fail the call")
	}
}

func TestATrailingSlashInTheBaseURLDoesNotBreakThePath(t *testing.T) {
	var seen string

	client := NewOSRMClient(osrmServer(t, http.StatusOK, okRoute, &seen).URL+"/", time.Second)
	if _, err := client.Route(context.Background(), erbil, airport); err != nil {
		t.Fatal(err)
	}

	if strings.Contains(seen, "//route") {
		t.Errorf("double slash in %q", seen)
	}
}

package routing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/pricing"
)

func TestAFareRouteGoesThroughItsStopsInOrder(t *testing.T) {
	var asked string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.Path
		_, _ = w.Write([]byte(`{"code":"Ok","routes":[{"distance":12500,"duration":1500}]}`))
	}))
	defer server.Close()

	route, err := NewOSRMClient(server.URL, time.Second).Route(context.Background(), 36.19, 44.01, 36.23, 43.96,
		pricing.Point{Latitude: 36.2, Longitude: 44.0}, pricing.Point{Latitude: 36.21, Longitude: 43.98})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasSuffix(asked, "/route/v1/driving/44.010000,36.190000;44.000000,36.200000;43.980000,36.210000;43.960000,36.230000") {
		t.Fatalf("asked %q", asked)
	}

	if route.DistanceKm != 12.5 || route.DurationMinutes != 25 || route.Estimated {
		t.Fatalf("route %+v", route)
	}
}

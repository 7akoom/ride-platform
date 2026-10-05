package clients

import (
	"context"
	"errors"
	"testing"
	"time"

	locationv1 "github.com/7akoom/ride-platform/gen/go/ride/location/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/7akoom/ride-platform/services/analytics/internal/domain"
)

type fakeLocation struct {
	locationv1.LocationServiceClient

	calls int
	err   error
}

func (f *fakeLocation) GetCity(_ context.Context, in *locationv1.GetCityRequest, _ ...grpc.CallOption) (*locationv1.CityResponse, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}

	return &locationv1.CityResponse{City: &locationv1.City{Id: in.GetCityId(), TimeZone: "Asia/Baghdad"}}, nil
}

func TestCitiesCachesTheTimeZone(t *testing.T) {
	location := &fakeLocation{}
	now := time.Now()
	cities := &Cities{location: location, cache: map[string]cachedZone{}, now: func() time.Time { return now }}

	for i := 0; i < 3; i++ {
		if zone, err := cities.TimeZone(context.Background(), "c1"); err != nil || zone != "Asia/Baghdad" {
			t.Fatalf("zone %q %v", zone, err)
		}
	}

	if location.calls != 1 {
		t.Fatalf("location asked %d times", location.calls)
	}

	now = now.Add(cityZoneCacheFor + time.Second)
	if _, err := cities.TimeZone(context.Background(), "c1"); err != nil || location.calls != 2 {
		t.Fatalf("expired entry not refreshed: %d %v", location.calls, err)
	}
}

func TestCitiesErrors(t *testing.T) {
	for code, want := range map[codes.Code]error{
		codes.NotFound:    domain.ErrUnknownCity,
		codes.Unavailable: domain.ErrUpstreamUnavailable,
		codes.Internal:    domain.ErrUpstreamUnavailable,
	} {
		cities := &Cities{location: &fakeLocation{err: status.Error(code, "x")}, cache: map[string]cachedZone{}, now: time.Now}
		if _, err := cities.TimeZone(context.Background(), "c1"); !errors.Is(err, want) {
			t.Errorf("%s: got %v", code, err)
		}
	}
}

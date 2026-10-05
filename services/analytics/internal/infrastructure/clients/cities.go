package clients

import (
	"context"
	"sync"
	"time"

	locationv1 "github.com/7akoom/ride-platform/gen/go/ride/location/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/7akoom/ride-platform/services/analytics/internal/application/query"
	"github.com/7akoom/ride-platform/services/analytics/internal/domain"
)

const (
	locationCallTimeout = 3 * time.Second
	cityZoneCacheFor    = 10 * time.Minute
)

// Cities reads a city's time zone from location-service (as a service) and
// keeps it for a few minutes.
type Cities struct {
	location locationv1.LocationServiceClient

	mu    sync.Mutex
	cache map[string]cachedZone
	now   func() time.Time
}

type cachedZone struct {
	zone    string
	expires time.Time
}

var _ query.Cities = (*Cities)(nil)

func NewCities(conn grpc.ClientConnInterface) *Cities {
	if conn == nil {
		panic("location-service connection is required")
	}

	return &Cities{location: locationv1.NewLocationServiceClient(conn), cache: map[string]cachedZone{}, now: time.Now}
}

func (c *Cities) TimeZone(ctx context.Context, cityID string) (string, error) {
	c.mu.Lock()
	hit, ok := c.cache[cityID]
	c.mu.Unlock()

	if ok && c.now().Before(hit.expires) {
		return hit.zone, nil
	}

	ctx, cancel := context.WithTimeout(ctx, locationCallTimeout)
	defer cancel()

	response, err := c.location.GetCity(ctx, &locationv1.GetCityRequest{CityId: cityID})

	switch status.Code(err) {
	case codes.OK:
	case codes.NotFound, codes.InvalidArgument:
		return "", domain.ErrUnknownCity
	default:
		return "", domain.ErrUpstreamUnavailable
	}

	zone := response.GetCity().GetTimeZone()
	if zone == "" {
		zone = "UTC"
	}

	c.mu.Lock()
	c.cache[cityID] = cachedZone{zone: zone, expires: c.now().Add(cityZoneCacheFor)}
	c.mu.Unlock()

	return zone, nil
}

package clients

import (
	"context"
	"fmt"

	locationv1 "github.com/7akoom/ride-platform/gen/go/ride/location/v1"
	"google.golang.org/grpc"
)

// countLimit caps how many nearby drivers we ask for. The demand tiers
// in service_surge.go only distinguish up to "more than 5", so fetching
// beyond a small number would be wasted work.
const countLimit = 20

type LocationClient struct {
	client locationv1.LocationServiceClient
}

func NewLocationClient(conn *grpc.ClientConn) *LocationClient {
	if conn == nil {
		panic("location-service connection is required")
	}

	return &LocationClient{client: locationv1.NewLocationServiceClient(conn)}
}

func (c *LocationClient) CountNearbyAvailableDrivers(
	ctx context.Context,
	latitude, longitude, radiusMeters float64,
) (int, error) {
	response, err := c.client.FindNearby(ctx, &locationv1.FindNearbyRequest{
		EntityType: locationv1.EntityType_ENTITY_TYPE_DRIVER,
		Coordinates: &locationv1.Coordinates{
			Latitude:  latitude,
			Longitude: longitude,
		},
		RadiusMeters: radiusMeters,
		Limit:        countLimit,
	})
	if err != nil {
		return 0, fmt.Errorf("call location-service FindNearby: %w", err)
	}

	return len(response.GetEntities()), nil
}

// CheckServiceZone reports whether a pickup point is served and, if
// so, which zone it resolved to — the same check trip-service runs
// before accepting a trip request. Used here to pick the right rate
// card and to refuse a quote for a location that could never become a
// real trip.
func (c *LocationClient) CheckServiceZone(
	ctx context.Context,
	latitude, longitude float64,
) (bool, string, error) {
	response, err := c.client.CheckServiceZone(ctx, &locationv1.CheckServiceZoneRequest{
		Coordinates: &locationv1.Coordinates{
			Latitude:  latitude,
			Longitude: longitude,
		},
	})
	if err != nil {
		return false, "", fmt.Errorf("call location-service CheckServiceZone: %w", err)
	}

	return response.GetServed(), response.GetZoneId(), nil
}

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

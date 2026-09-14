package clients

import (
	"context"
	"fmt"

	locationv1 "github.com/7akoom/ride-platform/gen/go/ride/location/v1"
	"github.com/7akoom/ride-platform/services/dispatch-service/internal/application/dispatch"
	"google.golang.org/grpc"
)

type LocationClient struct {
	client locationv1.LocationServiceClient
}

func NewLocationClient(conn *grpc.ClientConn) *LocationClient {
	if conn == nil {
		panic("location-service connection is required")
	}

	return &LocationClient{client: locationv1.NewLocationServiceClient(conn)}
}

func (c *LocationClient) FindNearbyDrivers(
	ctx context.Context,
	latitude, longitude, radiusMeters float64,
	limit int,
) ([]dispatch.NearbyDriver, error) {
	response, err := c.client.FindNearby(ctx, &locationv1.FindNearbyRequest{
		EntityType: locationv1.EntityType_ENTITY_TYPE_DRIVER,
		Coordinates: &locationv1.Coordinates{
			Latitude:  latitude,
			Longitude: longitude,
		},
		RadiusMeters: radiusMeters,
		Limit:        int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("call location-service FindNearby: %w", err)
	}

	results := make([]dispatch.NearbyDriver, len(response.GetEntities()))

	for i, entity := range response.GetEntities() {
		results[i] = dispatch.NearbyDriver{
			DriverID:       entity.GetEntityId(),
			DistanceMeters: entity.GetDistanceMeters(),
		}
	}

	return results, nil
}

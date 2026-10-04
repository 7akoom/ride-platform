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
			Latitude:       entity.GetCoordinates().GetLatitude(),
			Longitude:      entity.GetCoordinates().GetLongitude(),
		}
	}

	return results, nil
}

// TravelTimes asks location-service how long by road from each origin to the
// destination (dispatch.TravelTimeClient).
func (c *LocationClient) TravelTimes(
	ctx context.Context,
	origins []dispatch.Point,
	destination dispatch.Point,
) ([]dispatch.TravelTime, error) {
	request := &locationv1.GetTravelTimesRequest{
		Origins:     make([]*locationv1.Coordinates, len(origins)),
		Destination: &locationv1.Coordinates{Latitude: destination.Latitude, Longitude: destination.Longitude},
	}

	for i, origin := range origins {
		request.Origins[i] = &locationv1.Coordinates{Latitude: origin.Latitude, Longitude: origin.Longitude}
	}

	response, err := c.client.GetTravelTimes(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("call location-service GetTravelTimes: %w", err)
	}

	times := make([]dispatch.TravelTime, len(response.GetTimes()))
	for i, t := range response.GetTimes() {
		times[i] = dispatch.TravelTime{
			Reachable:       t.GetReachable(),
			DurationSeconds: t.GetDurationSeconds(),
			DistanceMeters:  t.GetDistanceMeters(),
		}
	}

	return times, nil
}

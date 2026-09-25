package clients

import (
	"context"
	"fmt"

	locationv1 "github.com/7akoom/ride-platform/gen/go/ride/location/v1"
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

func (c *LocationClient) CheckServiceZone(
	ctx context.Context,
	latitude, longitude float64,
) (bool, error) {
	response, err := c.client.CheckServiceZone(ctx, &locationv1.CheckServiceZoneRequest{
		Coordinates: &locationv1.Coordinates{
			Latitude:  latitude,
			Longitude: longitude,
		},
	})
	if err != nil {
		return false, fmt.Errorf("call location-service CheckServiceZone: %w", err)
	}

	return response.GetServed(), nil
}

// Locate is CheckServiceZone with the pickup city's time zone (for trips
// booked ahead).
func (c *LocationClient) Locate(ctx context.Context, latitude, longitude float64) (bool, string, error) {
	response, err := c.client.CheckServiceZone(ctx, &locationv1.CheckServiceZoneRequest{
		Coordinates: &locationv1.Coordinates{Latitude: latitude, Longitude: longitude},
	})
	if err != nil {
		return false, "", fmt.Errorf("call location-service CheckServiceZone: %w", err)
	}

	return response.GetServed(), response.GetTimeZone(), nil
}

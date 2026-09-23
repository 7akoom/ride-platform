package clients

import (
	"context"
	"fmt"

	locationv1 "github.com/7akoom/ride-platform/gen/go/ride/location/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/pricing"
)

// LocationClient is pricing-service's view of location-service: whether a
// pickup is served (and in which zone, city and time zone), who is near it,
// and whether a city or zone staff name exists.
type LocationClient struct {
	client locationv1.LocationServiceClient
}

func NewLocationClient(conn grpc.ClientConnInterface) *LocationClient {
	if conn == nil {
		panic("location-service connection is required")
	}

	return &LocationClient{client: locationv1.NewLocationServiceClient(conn)}
}

// CheckServiceZone reports whether a pickup point is served and, if so,
// which zone and city it resolved to — the same check trip-service runs
// before accepting a trip request. Used here to pick the right rate card
// and surge rules and to refuse a quote for a location that could never
// become a real trip.
func (c *LocationClient) CheckServiceZone(
	ctx context.Context,
	latitude, longitude float64,
) (pricing.ServiceZone, error) {
	response, err := c.client.CheckServiceZone(ctx, &locationv1.CheckServiceZoneRequest{
		Coordinates: &locationv1.Coordinates{
			Latitude:  latitude,
			Longitude: longitude,
		},
	})
	if err != nil {
		return pricing.ServiceZone{}, fmt.Errorf("call location-service CheckServiceZone: %w", err)
	}

	return pricing.ServiceZone{
		Served:   response.GetServed(),
		ZoneID:   response.GetZoneId(),
		CityID:   response.GetCityId(),
		TimeZone: response.GetTimeZone(),
	}, nil
}

// nearbyDrivers returns the drivers seen near a point in the last few
// seconds, nearest first, whether free or not.
func (c *LocationClient) nearbyDrivers(
	ctx context.Context,
	latitude, longitude, radiusMeters float64,
	limit int,
) ([]*locationv1.NearbyEntity, error) {
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

	return response.GetEntities(), nil
}

// CityExists is true for any city, active or not (the internal token sees
// inactive ones): staff may price a city before it opens.
func (c *LocationClient) CityExists(ctx context.Context, cityID string) (bool, error) {
	_, err := c.client.GetCity(ctx, &locationv1.GetCityRequest{CityId: cityID})

	return existence(err, "GetCity")
}

func (c *LocationClient) ZoneExists(ctx context.Context, zoneID string) (bool, error) {
	_, err := c.client.GetZone(ctx, &locationv1.GetZoneRequest{ZoneId: zoneID})

	return existence(err, "GetZone")
}

func existence(err error, method string) (bool, error) {
	switch status.Code(err) {
	case codes.OK:
		return true, nil
	case codes.NotFound, codes.InvalidArgument:
		return false, nil
	default:
		return false, fmt.Errorf("call location-service %s: %w", method, err)
	}
}

package clients

import (
	"context"
	"fmt"

	locationv1 "github.com/7akoom/ride-platform/gen/go/ride/location/v1"
	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// DriverLocation returns where a driver last reported being, as
// location-service knows it. It calls with the internal service token: the
// rider is not allowed to read a driver's position from location-service
// directly, and trip-service has already checked that this rider is on this
// trip. location-service forgets a position 30 seconds after the last ping, so
// a missing one is the expected "driver has not reported recently" answer.
func (c *LocationClient) DriverLocation(
	ctx context.Context,
	driverID string,
) (trip.DriverLocation, error) {
	response, err := c.client.GetLocation(ctx, &locationv1.GetLocationRequest{
		EntityType: locationv1.EntityType_ENTITY_TYPE_DRIVER,
		EntityId:   driverID,
	})
	if status.Code(err) == codes.NotFound {
		return trip.DriverLocation{}, trip.ErrDriverLocationUnavailable
	}

	if err != nil {
		return trip.DriverLocation{}, fmt.Errorf("call location-service GetLocation: %w", err)
	}

	coordinates := response.GetCoordinates()

	return trip.DriverLocation{
		Latitude:  coordinates.GetLatitude(),
		Longitude: coordinates.GetLongitude(),
		UpdatedAt: response.GetUpdatedAt().AsTime(),
	}, nil
}

package clients

import (
	"context"
	"fmt"

	tripv1 "github.com/7akoom/ride-platform/gen/go/ride/trip/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// DriverOfTrip returns the driver of the trip, or "" when the trip has none (or is
// gone).
func (c *TripClient) DriverOfTrip(ctx context.Context, tripID string) (string, error) {
	response, err := c.client.GetTrip(ctx, &tripv1.GetTripRequest{TripId: tripID})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return "", nil
		}

		return "", fmt.Errorf("call trip-service GetTrip: %w", err)
	}

	return response.GetTrip().GetDriverId(), nil
}

// HasActiveTrip reports whether the driver has an accepted or in-progress trip.
func (c *TripClient) HasActiveTrip(ctx context.Context, driverID string) (bool, error) {
	_, err := c.client.GetActiveTrip(ctx, &tripv1.GetActiveTripRequest{DriverId: driverID})

	switch status.Code(err) {
	case codes.OK:
		return true, nil

	case codes.NotFound:
		return false, nil

	default:
		return false, fmt.Errorf("call trip-service GetActiveTrip: %w", err)
	}
}

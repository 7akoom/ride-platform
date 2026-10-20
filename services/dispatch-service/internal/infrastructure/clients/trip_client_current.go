package clients

import (
	"context"
	"fmt"

	tripv1 "github.com/7akoom/ride-platform/gen/go/ride/trip/v1"
	"github.com/7akoom/ride-platform/services/dispatch-service/internal/application/dispatch"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// CurrentTrip is the driver's current trip as next trips need it. See
// dispatch.CurrentTripReader.
func (c *TripClient) CurrentTrip(ctx context.Context, driverID string) (dispatch.CurrentTrip, bool, error) {
	response, err := c.client.GetActiveTrip(ctx, &tripv1.GetActiveTripRequest{DriverId: driverID})

	switch status.Code(err) {
	case codes.OK:
	case codes.NotFound:
		return dispatch.CurrentTrip{}, false, nil
	default:
		return dispatch.CurrentTrip{}, false, fmt.Errorf("call trip-service GetActiveTrip: %w", err)
	}

	trip := response.GetTrip()

	current := dispatch.CurrentTrip{
		TripID:      trip.GetId(),
		InProgress:  trip.GetStatus() == tripv1.TripStatus_TRIP_STATUS_IN_PROGRESS,
		HasNextTrip: response.GetNextTrip() != nil,
		Dropoff:     dispatch.Point{Latitude: trip.GetDropoff().GetLatitude(), Longitude: trip.GetDropoff().GetLongitude()},
	}

	for _, stop := range trip.GetStops() {
		if stop.GetReachedAt() == nil {
			current.RemainingStops = append(current.RemainingStops, dispatch.Point{
				Latitude:  stop.GetCoordinates().GetLatitude(),
				Longitude: stop.GetCoordinates().GetLongitude(),
			})
		}
	}

	return current, true, nil
}

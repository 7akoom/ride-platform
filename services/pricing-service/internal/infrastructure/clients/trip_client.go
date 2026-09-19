package clients

import (
	"context"
	"fmt"

	tripv1 "github.com/7akoom/ride-platform/gen/go/ride/trip/v1"
	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/events"
	"google.golang.org/grpc"
)

// TripClient is pricing-service's view of trip-service: just enough of a
// trip to price it after it completes.
type TripClient struct {
	client tripv1.TripServiceClient
}

func NewTripClient(conn *grpc.ClientConn) *TripClient {
	if conn == nil {
		panic("trip-service connection is required")
	}

	return &TripClient{client: tripv1.NewTripServiceClient(conn)}
}

func (c *TripClient) GetTrip(
	ctx context.Context,
	tripID string,
) (events.TripInfo, error) {
	response, err := c.client.GetTrip(ctx, &tripv1.GetTripRequest{TripId: tripID})
	if err != nil {
		return events.TripInfo{}, fmt.Errorf("call trip-service GetTrip: %w", err)
	}

	trip := response.GetTrip()

	return events.TripInfo{
		ID:           trip.GetId(),
		RiderID:      trip.GetRiderId(),
		PickupLat:    trip.GetPickup().GetLatitude(),
		PickupLng:    trip.GetPickup().GetLongitude(),
		DropoffLat:   trip.GetDropoff().GetLatitude(),
		DropoffLng:   trip.GetDropoff().GetLongitude(),
		VehicleClass: trip.GetVehicleClass(),
	}, nil
}

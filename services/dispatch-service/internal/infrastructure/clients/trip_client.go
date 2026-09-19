package clients

import (
	"context"
	"fmt"

	tripv1 "github.com/7akoom/ride-platform/gen/go/ride/trip/v1"
	"github.com/7akoom/ride-platform/services/dispatch-service/internal/application/dispatch"
	"google.golang.org/grpc"
)

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
) (dispatch.TripInfo, error) {
	response, err := c.client.GetTrip(ctx, &tripv1.GetTripRequest{TripId: tripID})
	if err != nil {
		return dispatch.TripInfo{}, fmt.Errorf("call trip-service GetTrip: %w", err)
	}

	trip := response.GetTrip()

	return dispatch.TripInfo{
		ID:           trip.GetId(),
		RiderID:      trip.GetRiderId(),
		Status:       tripStatusToString(trip.GetStatus()),
		PickupLat:    trip.GetPickup().GetLatitude(),
		PickupLng:    trip.GetPickup().GetLongitude(),
		VehicleClass: trip.GetVehicleClass(),
	}, nil
}

func (c *TripClient) AcceptTrip(
	ctx context.Context,
	tripID string,
	driverID string,
) error {
	_, err := c.client.AcceptTrip(ctx, &tripv1.AcceptTripRequest{
		TripId:   tripID,
		DriverId: driverID,
	})
	if err != nil {
		return fmt.Errorf("call trip-service AcceptTrip: %w", err)
	}

	return nil
}

func tripStatusToString(s tripv1.TripStatus) string {
	switch s {
	case tripv1.TripStatus_TRIP_STATUS_REQUESTED:
		return "requested"
	case tripv1.TripStatus_TRIP_STATUS_ACCEPTED:
		return "accepted"
	case tripv1.TripStatus_TRIP_STATUS_IN_PROGRESS:
		return "in_progress"
	case tripv1.TripStatus_TRIP_STATUS_COMPLETED:
		return "completed"
	case tripv1.TripStatus_TRIP_STATUS_CANCELLED:
		return "cancelled"
	default:
		return "unspecified"
	}
}

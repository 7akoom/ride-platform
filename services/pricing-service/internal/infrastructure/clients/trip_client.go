package clients

import (
	"context"
	"fmt"
	"time"

	tripv1 "github.com/7akoom/ride-platform/gen/go/ride/trip/v1"
	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/events"
	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/pricing"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
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
		QuoteID:      trip.GetQuoteId(),
		Stops:        tripStops(trip.GetStops()),
		DriverID:     trip.GetDriverId(),
		Status:       tripStatus(trip.GetStatus()),
		CancelledBy:  trip.GetCancelledBy(),
		RiderNoShow:  trip.GetRiderNoShow(),
		AcceptedAt:   feeClockStart(trip),
		ArrivedAt:    optionalTime(trip.GetArrivedAt()),
		StartedAt:    optionalTime(trip.GetStartedAt()),
		CancelledAt:  optionalTime(trip.GetCancelledAt()),
	}, nil
}

// feeClockStart is the acceptance time the cancellation fee counts from: when the
// driver set off for the pickup. A driver who took the trip while finishing
// another one sets off when that one ends; until then (and on a trip cancelled
// before) there is none, so cancelling costs the rider nothing.
func feeClockStart(trip *tripv1.Trip) *time.Time {
	if trip.GetWaitingForPreviousTrip() {
		return nil
	}

	if at := optionalTime(trip.GetHeadingToPickupAt()); at != nil {
		return at
	}

	return optionalTime(trip.GetAcceptedAt())
}

func tripStops(stops []*tripv1.TripStop) []pricing.Point {
	var out []pricing.Point
	for _, stop := range stops {
		out = append(out, pricing.Point{Latitude: stop.GetCoordinates().GetLatitude(), Longitude: stop.GetCoordinates().GetLongitude()})
	}

	return out
}

func optionalTime(ts *timestamppb.Timestamp) *time.Time {
	if ts == nil {
		return nil
	}

	at := ts.AsTime()

	return &at
}

func tripStatus(status tripv1.TripStatus) string {
	switch status {
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
		return ""
	}
}

// TripSnapshot is the trip as the terms of waiting and cancelling need it.
// See pricing.TripReader.
func (c *TripClient) TripSnapshot(ctx context.Context, tripID string) (pricing.TripSnapshot, error) {
	response, err := c.client.GetTrip(ctx, &tripv1.GetTripRequest{TripId: tripID})

	switch status.Code(err) {
	case codes.OK:
	case codes.NotFound, codes.InvalidArgument:
		return pricing.TripSnapshot{}, pricing.ErrTripTermsNotFound
	default:
		return pricing.TripSnapshot{}, fmt.Errorf("call trip-service GetTrip: %w", err)
	}

	trip := response.GetTrip()

	return pricing.TripSnapshot{
		RiderID:      trip.GetRiderId(),
		DriverID:     trip.GetDriverId(),
		QuoteID:      trip.GetQuoteId(),
		VehicleClass: trip.GetVehicleClass(),
		PickupLat:    trip.GetPickup().GetLatitude(),
		PickupLng:    trip.GetPickup().GetLongitude(),
		AcceptedAt:   feeClockStart(trip),
		ArrivedAt:    optionalTime(trip.GetArrivedAt()),
	}, nil
}

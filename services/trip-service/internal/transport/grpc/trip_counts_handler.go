package grpc

import (
	"context"

	tripv1 "github.com/7akoom/ride-platform/gen/go/ride/trip/v1"
	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// riderTripCounter and driverTripCounter are what trip.WithTripCounts adds.
type riderTripCounter interface {
	RiderCompletedTrips(ctx context.Context, riderID string) (int, error)
}

type driverTripCounter interface {
	DriverCompletedTripsOfTrip(ctx context.Context, tripID string) (int, error)
}

// GetRiderTripStats counts the rider's completed trips. The interceptor has already
// checked that the rider is the caller.
func (h *TripHandler) GetRiderTripStats(
	ctx context.Context,
	request *tripv1.GetRiderTripStatsRequest,
) (*tripv1.GetRiderTripStatsResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	counter, ok := trip.As[riderTripCounter](h.tripService)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "trip counts are not configured")
	}

	count, err := counter.RiderCompletedTrips(ctx, request.GetRiderId())
	if err != nil {
		return nil, h.mapTripError(err)
	}

	return &tripv1.GetRiderTripStatsResponse{CompletedTrips: int32(count)}, nil
}

// driverCompletedTrips is the count on the driver card; 0 (left out) when it cannot be
// read, since the rest of the card is still worth showing.
func (h *TripHandler) driverCompletedTrips(ctx context.Context, tripID string) int32 {
	counter, ok := trip.As[driverTripCounter](h.tripService)
	if !ok {
		return 0
	}

	count, err := counter.DriverCompletedTripsOfTrip(ctx, tripID)
	if err != nil {
		h.logger.WarnContext(ctx, "could not count the driver's trips", "trip_id", tripID, "error", err)

		return 0
	}

	return int32(count)
}

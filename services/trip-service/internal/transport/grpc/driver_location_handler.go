package grpc

import (
	"context"
	"errors"

	tripv1 "github.com/7akoom/ride-platform/gen/go/ride/trip/v1"
	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// driverTracker is what trip.WithDriverTracking adds to a trip.Service. The
// handler asks for it as an optional capability, so the Service interface and
// every fake of it stay as they are.
type driverTracker interface {
	GetDriverLocation(ctx context.Context, tripID string) (trip.DriverLocation, error)
}

// GetDriverLocation is the rider app's live-tracking poll target. The
// interceptor has already checked that the caller is the rider of this trip.
func (h *TripHandler) GetDriverLocation(
	ctx context.Context,
	request *tripv1.GetDriverLocationRequest,
) (*tripv1.GetDriverLocationResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	tracker, ok := trip.As[driverTracker](h.tripService)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "driver tracking is not configured")
	}

	found, err := tracker.GetDriverLocation(ctx, request.GetTripId())

	switch {
	case errors.Is(err, trip.ErrTripNotTrackable):
		return nil, status.Error(codes.FailedPrecondition, err.Error())

	case errors.Is(err, trip.ErrDriverLocationUnavailable):
		return nil, status.Error(codes.NotFound, err.Error())

	case err != nil:
		return nil, h.mapTripError(err)
	}

	return &tripv1.GetDriverLocationResponse{
		Location: &tripv1.Coordinates{
			Latitude:  found.Latitude,
			Longitude: found.Longitude,
		},
		UpdatedAt: timestamppb.New(found.UpdatedAt),
	}, nil
}

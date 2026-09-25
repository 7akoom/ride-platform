package grpc

import (
	"context"

	tripv1 "github.com/7akoom/ride-platform/gen/go/ride/trip/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

type stopArrival interface {
	ReachStop(ctx context.Context, tripID string, position int) (trip.Trip, error)
}

// ReachStop is the driver marking one of the trip's stops as reached. The
// interceptor has already checked that the caller is the trip's driver.
func (h *TripHandler) ReachStop(
	ctx context.Context,
	request *tripv1.ReachStopRequest,
) (*tripv1.ReachStopResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	stops, ok := trip.As[stopArrival](h.tripService)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "marking stops is not available")
	}

	reached, err := stops.ReachStop(ctx, request.GetTripId(), int(request.GetPosition()))
	if err != nil {
		return nil, h.mapTripError(err)
	}

	return &tripv1.ReachStopResponse{Trip: toProtoTrip(reached)}, nil
}

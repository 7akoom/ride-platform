package grpc

import (
	"context"

	tripv1 "github.com/7akoom/ride-platform/gen/go/ride/trip/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

type driverArrival interface {
	MarkDriverArrived(ctx context.Context, tripID string) (trip.Trip, error)
}

func (h *TripHandler) MarkDriverArrived(
	ctx context.Context,
	request *tripv1.MarkDriverArrivedRequest,
) (*tripv1.MarkDriverArrivedResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	arrivals, ok := trip.As[driverArrival](h.tripService)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "marking arrival is not available")
	}

	arrived, err := arrivals.MarkDriverArrived(ctx, request.GetTripId())
	if err != nil {
		return nil, h.mapTripError(err)
	}

	return &tripv1.MarkDriverArrivedResponse{Trip: toProtoTrip(arrived)}, nil
}

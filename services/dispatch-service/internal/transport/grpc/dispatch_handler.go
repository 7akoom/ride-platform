package grpc

import (
	"context"
	"errors"

	dispatchv1 "github.com/7akoom/ride-platform/gen/go/ride/dispatch/v1"
	"github.com/7akoom/ride-platform/services/dispatch-service/internal/application/dispatch"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type DispatchHandler struct {
	dispatchv1.UnimplementedDispatchServiceServer

	dispatchService dispatch.Service
}

func NewDispatchHandler(
	dispatchService dispatch.Service,
) *DispatchHandler {
	if dispatchService == nil {
		panic("dispatch service is required")
	}

	return &DispatchHandler{
		dispatchService: dispatchService,
	}
}

func (h *DispatchHandler) DispatchTrip(
	ctx context.Context,
	request *dispatchv1.DispatchTripRequest,
) (*dispatchv1.DispatchTripResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	result, err := h.dispatchService.DispatchTrip(
		ctx,
		request.GetTripId(),
		request.GetSearchRadiusMeters(),
	)
	if err != nil {
		return nil, mapDispatchError(err)
	}

	return &dispatchv1.DispatchTripResponse{
		TripId:         result.TripID,
		DriverId:       result.DriverID,
		DistanceMeters: result.DistanceMeters,
	}, nil
}

func mapDispatchError(err error) error {
	switch {
	case errors.Is(err, dispatch.ErrTripIDRequired):
		return status.Error(codes.InvalidArgument, err.Error())

	case errors.Is(err, dispatch.ErrTripNotDispatchable):
		return status.Error(codes.FailedPrecondition, err.Error())

	case errors.Is(err, dispatch.ErrNoDriversNearby),
		errors.Is(err, dispatch.ErrNoDriversAvailable):
		return status.Error(codes.NotFound, err.Error())

	default:
		return status.Error(codes.Internal, "failed to process dispatch request")
	}
}

package grpc

import (
	"context"
	"errors"

	tripv1 "github.com/7akoom/ride-platform/gen/go/ride/trip/v1"
	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// driverSummarizer is what trip.WithDriverProfile adds to a trip.Service. The handler asks
// for it as an optional capability, like the driver tracker.
type driverSummarizer interface {
	GetTripDriver(ctx context.Context, tripID string) (trip.DriverSummary, error)
}

// GetTripDriver is how the rider's app shows who is driving: name, car, plate and rating.
// The interceptor has already checked that the caller is the rider of this trip.
func (h *TripHandler) GetTripDriver(
	ctx context.Context,
	request *tripv1.GetTripDriverRequest,
) (*tripv1.GetTripDriverResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	summarizer, ok := trip.As[driverSummarizer](h.tripService)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "driver profiles are not configured")
	}

	summary, err := summarizer.GetTripDriver(ctx, request.GetTripId())

	switch {
	case errors.Is(err, trip.ErrTripHasNoDriver):
		return nil, status.Error(codes.FailedPrecondition, err.Error())

	case errors.Is(err, trip.ErrDriverProfileUnavailable):
		return nil, status.Error(codes.NotFound, err.Error())

	case err != nil:
		return nil, h.mapTripError(err)
	}

	return &tripv1.GetTripDriverResponse{
		Driver: &tripv1.TripDriver{
			DisplayName: summary.DisplayName,
			Vehicle: &tripv1.TripVehicle{
				Make:         summary.VehicleMake,
				Model:        summary.VehicleModel,
				Color:        summary.VehicleColor,
				PlateNumber:  summary.PlateNumber,
				VehicleClass: summary.VehicleClass,
			},
			RatingAverage: summary.RatingAverage,
			RatingCount:   summary.RatingCount,
		},
	}, nil
}

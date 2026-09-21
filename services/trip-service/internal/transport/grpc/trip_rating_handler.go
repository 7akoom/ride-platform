package grpc

import (
	"context"

	tripv1 "github.com/7akoom/ride-platform/gen/go/ride/trip/v1"
	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// RateTrip records one side's rating of the other. Which side is speaking is named in
// the request and checked against the trip by the authorization layer.
func (h *TripHandler) RateTrip(
	ctx context.Context,
	request *tripv1.RateTripRequest,
) (*tripv1.RateTripResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	_, err := h.tripService.RateTrip(ctx, trip.RateTripInput{
		TripID:  request.GetTripId(),
		RatedBy: toDomainRatedBy(request.GetRatedBy()),
		Stars:   int(request.GetStars()),
		Comment: request.GetComment(),
	})
	if err != nil {
		return nil, h.mapTripError(err)
	}

	return &tripv1.RateTripResponse{}, nil
}

func toDomainRatedBy(r tripv1.RatedBy) trip.RatedBy {
	switch r {
	case tripv1.RatedBy_RATED_BY_RIDER:
		return trip.RatedByRider
	case tripv1.RatedBy_RATED_BY_DRIVER:
		return trip.RatedByDriver
	default:
		return ""
	}
}

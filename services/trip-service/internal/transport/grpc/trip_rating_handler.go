package grpc

import (
	"context"

	tripv1 "github.com/7akoom/ride-platform/gen/go/ride/trip/v1"
	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
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
		Tags:    toDomainRatingTags(request.GetTags()),
	})
	if err != nil {
		return nil, h.mapTripError(err)
	}

	return &tripv1.RateTripResponse{}, nil
}

// GetTripRating is one side's own rating of a trip and whether it can still be given.
// The side is checked against the trip by the authorization layer, as for RateTrip.
func (h *TripHandler) GetTripRating(
	ctx context.Context,
	request *tripv1.GetTripRatingRequest,
) (*tripv1.GetTripRatingResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	got, err := h.tripService.TripRating(ctx, request.GetTripId(), toDomainRatedBy(request.GetRatedBy()))
	if err != nil {
		return nil, h.mapTripError(err)
	}

	response := &tripv1.GetTripRatingResponse{
		Rated:   got.Rated,
		Stars:   int32(got.Stars),
		CanRate: got.CanRate,
	}

	if got.CanRateUntil != nil {
		response.CanRateUntil = timestamppb.New(*got.CanRateUntil)
	}

	return response, nil
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

// toDomainRatingTags names the tags; an unknown one stays unknown, so the service
// refuses it.
func toDomainRatingTags(tags []tripv1.RatingTag) []string {
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		switch tag {
		case tripv1.RatingTag_RATING_TAG_SAFE_DRIVING:
			out = append(out, trip.TagSafeDriving)
		case tripv1.RatingTag_RATING_TAG_CLEAN_CAR:
			out = append(out, trip.TagCleanCar)
		case tripv1.RatingTag_RATING_TAG_FRIENDLY:
			out = append(out, trip.TagFriendly)
		case tripv1.RatingTag_RATING_TAG_KNOWS_THE_WAY:
			out = append(out, trip.TagKnowsTheWay)
		case tripv1.RatingTag_RATING_TAG_ON_TIME:
			out = append(out, trip.TagOnTime)
		case tripv1.RatingTag_RATING_TAG_POLITE:
			out = append(out, trip.TagPolite)
		default:
			out = append(out, "unknown")
		}
	}

	return out
}

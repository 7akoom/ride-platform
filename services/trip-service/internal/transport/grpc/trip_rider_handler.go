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

// riderSummarizer is what trip.WithRiderProfile adds to a trip.Service.
type riderSummarizer interface {
	GetTripRider(ctx context.Context, tripID string) (trip.RiderSummary, error)
}

// driverTermsReader is what trip.WithDriverTerms adds to a trip.Service.
type driverTermsReader interface {
	GetDriverTripTerms(ctx context.Context, tripID string) (trip.DriverTerms, error)
}

// GetTripRider is how the Captain app shows who it is picking up: first name and
// rating. The interceptor has already checked that the caller drives this trip.
func (h *TripHandler) GetTripRider(
	ctx context.Context,
	request *tripv1.GetTripRiderRequest,
) (*tripv1.GetTripRiderResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	summarizer, ok := trip.As[riderSummarizer](h.tripService)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "rider profiles are not configured")
	}

	summary, err := summarizer.GetTripRider(ctx, request.GetTripId())

	switch {
	case errors.Is(err, trip.ErrRiderProfileUnavailable):
		return nil, status.Error(codes.NotFound, err.Error())
	case err != nil:
		return nil, h.mapTripError(err)
	}

	return &tripv1.GetTripRiderResponse{Rider: &tripv1.TripRider{
		FirstName:     summary.FirstName,
		RatingAverage: summary.RatingAverage,
		RatingCount:   summary.RatingCount,
	}}, nil
}

// GetDriverTripTerms is what the driver needs to wait at the pickup and act on time.
// The interceptor has already checked that the caller drives this trip.
func (h *TripHandler) GetDriverTripTerms(
	ctx context.Context,
	request *tripv1.GetDriverTripTermsRequest,
) (*tripv1.GetDriverTripTermsResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	reader, ok := trip.As[driverTermsReader](h.tripService)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "trip terms are not configured")
	}

	terms, err := reader.GetDriverTripTerms(ctx, request.GetTripId())

	switch {
	case errors.Is(err, trip.ErrDriverTermsUnavailable):
		return nil, status.Error(codes.NotFound, err.Error())
	case err != nil:
		return nil, h.mapTripError(err)
	}

	response := &tripv1.GetDriverTripTermsResponse{
		CurrencyCode:        terms.CurrencyCode,
		FreeWaitingMinutes:  int32(terms.FreeWaitingMinutes),
		WaitingPerMinute:    terms.WaitingPerMinute,
		NoShowWaitMinutes:   int32(terms.NoShowWaitMinutes),
		NoShowFee:           terms.NoShowFee,
		ArrivalRadiusMeters: int32(terms.ArrivalRadiusMeters),
	}

	if terms.FreeWaitingUntil != nil {
		response.FreeWaitingUntil = timestamppb.New(*terms.FreeWaitingUntil)
	}

	if terms.NoShowFrom != nil {
		response.NoShowFrom = timestamppb.New(*terms.NoShowFrom)
	}

	return response, nil
}

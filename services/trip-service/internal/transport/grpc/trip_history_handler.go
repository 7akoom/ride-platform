package grpc

import (
	"context"
	"errors"

	tripv1 "github.com/7akoom/ride-platform/gen/go/ride/trip/v1"
	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// tripHistory is what trip.WithTripHistory adds to a trip.Service. The handler
// asks for it as an optional capability (trip.As), so the Service interface and
// every fake of it stay as they are.
type tripHistory interface {
	GetActiveTrip(ctx context.Context, riderID string, driverID string) (trip.Trip, error)
	ListTrips(ctx context.Context, query trip.HistoryQuery) (trip.HistoryPage, error)
}

// GetActiveTrip returns the caller's requested, accepted or in-progress trip: what
// an app shows when it is opened again. The interceptor has already checked that
// the profile named is the caller's own.
func (h *TripHandler) GetActiveTrip(
	ctx context.Context,
	request *tripv1.GetActiveTripRequest,
) (*tripv1.GetActiveTripResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	history, ok := trip.As[tripHistory](h.tripService)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "trip history is not configured")
	}

	found, err := history.GetActiveTrip(ctx, request.GetRiderId(), request.GetDriverId())

	switch {
	case errors.Is(err, trip.ErrInvalidHistoryQuery):
		return nil, status.Error(codes.InvalidArgument, err.Error())

	case errors.Is(err, trip.ErrTripNotFound):
		return nil, status.Error(codes.NotFound, "no active trip")

	case err != nil:
		return nil, h.mapTripError(err)
	}

	return &tripv1.GetActiveTripResponse{Trip: toProtoTrip(found)}, nil
}

// ListTrips returns one page of the caller's trips, newest first. The interceptor
// has already checked that the profile named is the caller's own.
func (h *TripHandler) ListTrips(
	ctx context.Context,
	request *tripv1.ListTripsRequest,
) (*tripv1.ListTripsResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	history, ok := trip.As[tripHistory](h.tripService)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "trip history is not configured")
	}

	page, err := history.ListTrips(ctx, trip.HistoryQuery{
		RiderID:   request.GetRiderId(),
		DriverID:  request.GetDriverId(),
		PageSize:  int(request.GetPageSize()),
		PageToken: request.GetPageToken(),
	})

	switch {
	case errors.Is(err, trip.ErrInvalidHistoryQuery),
		errors.Is(err, trip.ErrInvalidPageSize),
		errors.Is(err, trip.ErrInvalidPageToken):
		return nil, status.Error(codes.InvalidArgument, err.Error())

	case err != nil:
		return nil, h.mapTripError(err)
	}

	trips := make([]*tripv1.Trip, 0, len(page.Trips))
	for _, item := range page.Trips {
		trips = append(trips, toProtoTrip(item))
	}

	return &tripv1.ListTripsResponse{Trips: trips, NextPageToken: page.NextPageToken}, nil
}

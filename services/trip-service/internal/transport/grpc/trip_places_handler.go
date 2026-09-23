package grpc

import (
	"context"
	"time"

	tripv1 "github.com/7akoom/ride-platform/gen/go/ride/trip/v1"
	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// pickupPhotos is what trip.WithPickupPhotos adds to a trip.Service.
type pickupPhotos interface {
	PickupPhoto(ctx context.Context, tripID string) (string, time.Time, error)
}

// recentDestinations is what trip.WithRecentDestinations adds.
type recentDestinations interface {
	RecentDestinations(ctx context.Context, riderID string, limit int) ([]trip.Destination, error)
}

// GetPickupPhoto gives the rider or the driver of the trip a short-lived link
// to the photo of the saved pickup address. The interceptor has already
// checked that the caller is one of them.
func (h *TripHandler) GetPickupPhoto(
	ctx context.Context,
	request *tripv1.GetPickupPhotoRequest,
) (*tripv1.GetPickupPhotoResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	photos, ok := trip.As[pickupPhotos](h.tripService)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "pickup photos are not configured")
	}

	url, expires, err := photos.PickupPhoto(ctx, request.GetTripId())
	if err != nil {
		return nil, h.mapTripError(err)
	}

	return &tripv1.GetPickupPhotoResponse{Url: url, ExpiresAt: timestamppb.New(expires)}, nil
}

// ListRecentDestinations lists where the rider's completed trips ended. The
// interceptor has already checked that the rider is the caller.
func (h *TripHandler) ListRecentDestinations(
	ctx context.Context,
	request *tripv1.ListRecentDestinationsRequest,
) (*tripv1.ListRecentDestinationsResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	recent, ok := trip.As[recentDestinations](h.tripService)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "recent destinations are not configured")
	}

	found, err := recent.RecentDestinations(ctx, request.GetRiderId(), int(request.GetLimit()))
	if err != nil {
		return nil, h.mapTripError(err)
	}

	out := make([]*tripv1.RecentDestination, 0, len(found))
	for _, d := range found {
		out = append(out, &tripv1.RecentDestination{
			Coordinates: &tripv1.Coordinates{Latitude: d.Coordinates.Latitude, Longitude: d.Coordinates.Longitude},
			Address:     d.Address,
			LastTripAt:  timestamppb.New(d.LastTripAt),
		})
	}

	return &tripv1.ListRecentDestinationsResponse{Destinations: out}, nil
}

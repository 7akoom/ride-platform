package grpc

import (
	"context"
	"errors"

	tripv1 "github.com/7akoom/ride-platform/gen/go/ride/trip/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/share"
)

var errSharesUnavailable = status.Error(codes.Unimplemented, "trip sharing is not available")

// ShareTrip makes a link to the trip. The interceptor has already checked
// that the caller is the trip's rider.
func (h *TripHandler) ShareTrip(ctx context.Context, request *tripv1.ShareTripRequest) (*tripv1.ShareTripResponse, error) {
	if h.shares == nil {
		return nil, errSharesUnavailable
	}

	made, err := h.shares.Create(ctx, request.GetTripId())
	if err != nil {
		return nil, h.mapShareError(err)
	}

	return &tripv1.ShareTripResponse{
		Token:     made.Token,
		Url:       made.URL,
		CreatedAt: timestamppb.New(made.CreatedAt),
		ExpiresAt: timestamppb.New(made.ExpiresAt),
	}, nil
}

// StopSharingTrip ends every link to the trip (its rider only).
func (h *TripHandler) StopSharingTrip(ctx context.Context, request *tripv1.StopSharingTripRequest) (*tripv1.StopSharingTripResponse, error) {
	if h.shares == nil {
		return nil, errSharesUnavailable
	}

	stopped, err := h.shares.Stop(ctx, request.GetTripId())
	if err != nil {
		return nil, h.mapShareError(err)
	}

	return &tripv1.StopSharingTripResponse{Stopped: int32(stopped)}, nil
}

// GetSharedTrip answers anyone with a link: its token is the credential.
func (h *TripHandler) GetSharedTrip(ctx context.Context, request *tripv1.GetSharedTripRequest) (*tripv1.GetSharedTripResponse, error) {
	if h.shares == nil {
		return nil, errSharesUnavailable
	}

	view, err := h.shares.View(ctx, request.GetToken())
	if err != nil {
		return nil, h.mapShareError(err)
	}

	return &tripv1.GetSharedTripResponse{Trip: toProtoSharedTrip(view)}, nil
}

func (h *TripHandler) mapShareError(err error) error {
	switch {
	case errors.Is(err, share.ErrNotFound):
		return status.Error(codes.NotFound, err.Error())

	case errors.Is(err, share.ErrTripNotLive),
		errors.Is(err, share.ErrTooManyLinks):
		return status.Error(codes.FailedPrecondition, err.Error())

	default:
		return h.mapTripError(err)
	}
}

// toProtoSharedTrip is the whole of what a link shows. Anything added here is
// shown to whoever has the link, without an account: never the rider, the
// passenger, the price or a phone number.
func toProtoSharedTrip(view share.View) *tripv1.SharedTrip {
	t := view.Trip

	out := &tripv1.SharedTrip{
		Status:         toProtoStatus(t.Status),
		Pickup:         &tripv1.Coordinates{Latitude: t.Pickup.Latitude, Longitude: t.Pickup.Longitude},
		Dropoff:        &tripv1.Coordinates{Latitude: t.Dropoff.Latitude, Longitude: t.Dropoff.Longitude},
		PickupAddress:  t.PickupAddress,
		DropoffAddress: t.DropoffAddress,
		Stops:          toProtoStops(t.Stops),
		RequestedAt:    timestamppb.New(t.RequestedAt),
		AcceptedAt:     optionalTimestamp(t.AcceptedAt),
		ArrivedAt:      optionalTimestamp(t.ArrivedAt),
		StartedAt:      optionalTimestamp(t.StartedAt),
		CompletedAt:    optionalTimestamp(t.CompletedAt),
		CancelledAt:    optionalTimestamp(t.CancelledAt),
		LinkExpiresAt:  timestamppb.New(view.ExpiresAt),
	}

	if d := view.Driver; d != nil {
		out.DriverName = d.DisplayName
		out.Vehicle = &tripv1.TripVehicle{
			Make: d.VehicleMake, Model: d.VehicleModel, Color: d.VehicleColor,
			PlateNumber: d.PlateNumber, VehicleClass: d.VehicleClass,
		}
	}

	if l := view.Location; l != nil {
		out.DriverLocation = &tripv1.Coordinates{Latitude: l.Latitude, Longitude: l.Longitude}
		out.DriverLocationUpdatedAt = timestamppb.New(l.UpdatedAt)
	}

	return out
}

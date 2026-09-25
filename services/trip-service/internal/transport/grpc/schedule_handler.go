package grpc

import (
	"context"
	"errors"

	tripv1 "github.com/7akoom/ride-platform/gen/go/ride/trip/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/schedule"
)

var errSchedulesUnavailable = status.Error(codes.Unimplemented, "scheduled trips are not available")

func (h *TripHandler) ScheduleTrip(
	ctx context.Context,
	request *tripv1.ScheduleTripRequest,
) (*tripv1.ScheduledTripResponse, error) {
	if h.schedules == nil {
		return nil, errSchedulesUnavailable
	}

	if request.GetScheduledAt() == nil {
		return nil, status.Error(codes.InvalidArgument, "scheduled_at is required")
	}

	booked, err := h.schedules.Book(ctx, schedule.BookInput{
		RiderID:               request.GetRiderId(),
		PickupLat:             request.GetPickup().GetLatitude(),
		PickupLng:             request.GetPickup().GetLongitude(),
		DropoffLat:            request.GetDropoff().GetLatitude(),
		DropoffLng:            request.GetDropoff().GetLongitude(),
		PickupAddress:         request.GetPickupAddress(),
		DropoffAddress:        request.GetDropoffAddress(),
		PickupSavedAddressID:  request.GetPickupSavedAddressId(),
		DropoffSavedAddressID: request.GetDropoffSavedAddressId(),
		VehicleClass:          request.GetVehicleClass(),
		PaymentMethod:         request.GetPaymentMethod(),
		PassengerName:         request.GetPassengerName(),
		PassengerPhone:        request.GetPassengerPhone(),
		ScheduledAt:           request.GetScheduledAt().AsTime(),
		IdempotencyKey:        request.GetIdempotencyKey(),
	})
	if err != nil {
		return nil, h.mapScheduleError(err)
	}

	return &tripv1.ScheduledTripResponse{ScheduledTrip: toProtoScheduledTrip(booked)}, nil
}

func (h *TripHandler) ListScheduledTrips(
	ctx context.Context,
	request *tripv1.ListScheduledTripsRequest,
) (*tripv1.ListScheduledTripsResponse, error) {
	if h.schedules == nil {
		return nil, errSchedulesUnavailable
	}

	rides, err := h.schedules.List(ctx, request.GetRiderId(), request.GetIncludePast())
	if err != nil {
		return nil, h.mapScheduleError(err)
	}

	response := &tripv1.ListScheduledTripsResponse{}
	for _, r := range rides {
		response.ScheduledTrips = append(response.ScheduledTrips, toProtoScheduledTrip(r))
	}

	return response, nil
}

func (h *TripHandler) CancelScheduledTrip(
	ctx context.Context,
	request *tripv1.CancelScheduledTripRequest,
) (*tripv1.ScheduledTripResponse, error) {
	if h.schedules == nil {
		return nil, errSchedulesUnavailable
	}

	cancelled, err := h.schedules.Cancel(ctx, request.GetScheduledTripId(), request.GetRiderId())
	if err != nil {
		return nil, h.mapScheduleError(err)
	}

	return &tripv1.ScheduledTripResponse{ScheduledTrip: toProtoScheduledTrip(cancelled)}, nil
}

func (h *TripHandler) mapScheduleError(err error) error {
	switch {
	case errors.Is(err, schedule.ErrNotFound):
		return status.Error(codes.NotFound, err.Error())

	case errors.Is(err, schedule.ErrKeyReused):
		return status.Error(codes.AlreadyExists, err.Error())

	case errors.Is(err, schedule.ErrTooManyUpcoming),
		errors.Is(err, schedule.ErrNotScheduled):
		return status.Error(codes.FailedPrecondition, err.Error())

	case errors.Is(err, schedule.ErrRiderRequired),
		errors.Is(err, schedule.ErrIdempotencyKey),
		errors.Is(err, schedule.ErrTooSoon),
		errors.Is(err, schedule.ErrTooFar):
		return status.Error(codes.InvalidArgument, err.Error())

	default:
		return h.mapTripError(err)
	}
}

func toProtoScheduledTrip(r schedule.Ride) *tripv1.ScheduledTrip {
	out := &tripv1.ScheduledTrip{
		Id:             r.ID,
		RiderId:        r.RiderID,
		Status:         string(r.Status),
		ScheduledAt:    timestamppb.New(r.ScheduledAt),
		TimeZone:       r.TimeZone,
		ScheduledLocal: r.Local(),
		Pickup:         &tripv1.Coordinates{Latitude: r.Pickup.Latitude, Longitude: r.Pickup.Longitude},
		Dropoff:        &tripv1.Coordinates{Latitude: r.Dropoff.Latitude, Longitude: r.Dropoff.Longitude},
		PickupAddress:  r.PickupAddress,
		DropoffAddress: r.DropoffAddress,
		VehicleClass:   r.VehicleClass,
		PaymentMethod:  r.PaymentMethod,
		PassengerName:  r.PassengerName,
		PassengerPhone: r.PassengerPhone,
		TripId:         r.TripID,
		CreatedAt:      timestamppb.New(r.CreatedAt),
		CancelledAt:    optionalTimestamp(r.CancelledAt),
		DispatchedAt:   optionalTimestamp(r.DispatchedAt),
	}

	if r.Status == schedule.Failed {
		out.FailureReason = r.LastError
	}

	return out
}

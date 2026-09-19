package grpc

import (
	"context"
	"errors"
	"log/slog"
	"time"

	tripv1 "github.com/7akoom/ride-platform/gen/go/ride/trip/v1"
	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type TripHandler struct {
	tripv1.UnimplementedTripServiceServer

	tripService trip.Service
	logger      *slog.Logger
}

func NewTripHandler(
	tripService trip.Service,
	logger *slog.Logger,
) *TripHandler {
	if tripService == nil {
		panic("trip service is required")
	}

	if logger == nil {
		panic("logger is required")
	}

	return &TripHandler{
		tripService: tripService,
		logger:      logger,
	}
}

func (h *TripHandler) RequestTrip(
	ctx context.Context,
	request *tripv1.RequestTripRequest,
) (*tripv1.RequestTripResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	pickup := request.GetPickup()
	dropoff := request.GetDropoff()

	created, err := h.tripService.RequestTrip(
		ctx,
		trip.RequestTripInput{
			RiderID:      request.GetRiderId(),
			PickupLat:    pickup.GetLatitude(),
			PickupLng:    pickup.GetLongitude(),
			DropoffLat:   dropoff.GetLatitude(),
			DropoffLng:   dropoff.GetLongitude(),
			VehicleClass: request.GetVehicleClass(),
		},
	)
	if err != nil {
		return nil, h.mapTripError(err)
	}

	return &tripv1.RequestTripResponse{
		Trip: toProtoTrip(created),
	}, nil
}

func (h *TripHandler) AcceptTrip(
	ctx context.Context,
	request *tripv1.AcceptTripRequest,
) (*tripv1.AcceptTripResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	accepted, err := h.tripService.AcceptTrip(ctx, request.GetTripId(), request.GetDriverId())
	if err != nil {
		return nil, h.mapTripError(err)
	}

	return &tripv1.AcceptTripResponse{
		Trip: toProtoTrip(accepted),
	}, nil
}

func (h *TripHandler) StartTrip(
	ctx context.Context,
	request *tripv1.StartTripRequest,
) (*tripv1.StartTripResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	started, err := h.tripService.StartTrip(ctx, request.GetTripId())
	if err != nil {
		return nil, h.mapTripError(err)
	}

	return &tripv1.StartTripResponse{
		Trip: toProtoTrip(started),
	}, nil
}

func (h *TripHandler) CompleteTrip(
	ctx context.Context,
	request *tripv1.CompleteTripRequest,
) (*tripv1.CompleteTripResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	completed, err := h.tripService.CompleteTrip(ctx, request.GetTripId())
	if err != nil {
		return nil, h.mapTripError(err)
	}

	return &tripv1.CompleteTripResponse{
		Trip: toProtoTrip(completed),
	}, nil
}

func (h *TripHandler) CancelTrip(
	ctx context.Context,
	request *tripv1.CancelTripRequest,
) (*tripv1.CancelTripResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	cancelled, err := h.tripService.CancelTrip(ctx, request.GetTripId(), request.GetReason())
	if err != nil {
		return nil, h.mapTripError(err)
	}

	return &tripv1.CancelTripResponse{
		Trip: toProtoTrip(cancelled),
	}, nil
}

func (h *TripHandler) GetTrip(
	ctx context.Context,
	request *tripv1.GetTripRequest,
) (*tripv1.GetTripResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	found, err := h.tripService.GetTrip(ctx, request.GetTripId())
	if err != nil {
		return nil, h.mapTripError(err)
	}

	return &tripv1.GetTripResponse{
		Trip: toProtoTrip(found),
	}, nil
}

func (h *TripHandler) TriggerSOS(
	ctx context.Context,
	request *tripv1.TriggerSOSRequest,
) (*tripv1.TriggerSOSResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	location := request.GetLocation()

	alertID, triggeredAt, err := h.tripService.TriggerSOS(
		ctx,
		request.GetTripId(),
		toDomainSosTriggeredBy(request.GetTriggeredBy()),
		location.GetLatitude(),
		location.GetLongitude(),
	)
	if err != nil {
		return nil, h.mapTripError(err)
	}

	return &tripv1.TriggerSOSResponse{
		AlertId:     alertID,
		TriggeredAt: timestamppb.New(triggeredAt),
	}, nil
}

func (h *TripHandler) RecordWaypoint(
	ctx context.Context,
	request *tripv1.RecordWaypointRequest,
) (*tripv1.RecordWaypointResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	location := request.GetLocation()

	if err := h.tripService.RecordWaypoint(
		ctx,
		request.GetTripId(),
		location.GetLatitude(),
		location.GetLongitude(),
	); err != nil {
		return nil, h.mapTripError(err)
	}

	return &tripv1.RecordWaypointResponse{}, nil
}

func (h *TripHandler) GetTripPath(
	ctx context.Context,
	request *tripv1.GetTripPathRequest,
) (*tripv1.GetTripPathResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	waypoints, err := h.tripService.GetTripPath(ctx, request.GetTripId())
	if err != nil {
		return nil, h.mapTripError(err)
	}

	protoWaypoints := make([]*tripv1.Waypoint, len(waypoints))

	for i, w := range waypoints {
		protoWaypoints[i] = &tripv1.Waypoint{
			Location: &tripv1.Coordinates{
				Latitude:  w.Coordinates.Latitude,
				Longitude: w.Coordinates.Longitude,
			},
			RecordedAt: timestamppb.New(w.RecordedAt),
		}
	}

	return &tripv1.GetTripPathResponse{
		Waypoints: protoWaypoints,
	}, nil
}

func toDomainSosTriggeredBy(t tripv1.SosTriggeredBy) trip.SosTriggeredBy {
	switch t {
	case tripv1.SosTriggeredBy_SOS_TRIGGERED_BY_RIDER:
		return trip.SosTriggeredByRider
	case tripv1.SosTriggeredBy_SOS_TRIGGERED_BY_DRIVER:
		return trip.SosTriggeredByDriver
	default:
		return ""
	}
}

func (h *TripHandler) mapTripError(err error) error {
	switch {
	case errors.Is(err, trip.ErrTripNotFound):
		return status.Error(codes.NotFound, "trip not found")

	case errors.Is(err, trip.ErrInvalidTransition):
		return status.Error(codes.FailedPrecondition, err.Error())

	case errors.Is(err, trip.ErrRiderHasActiveTrip),
		errors.Is(err, trip.ErrDriverHasActiveTrip):
		return status.Error(codes.FailedPrecondition, err.Error())

	case errors.Is(err, trip.ErrRiderIDRequired),
		errors.Is(err, trip.ErrDriverIDRequired),
		errors.Is(err, trip.ErrTripIDRequired),
		errors.Is(err, trip.ErrInvalidLatitude),
		errors.Is(err, trip.ErrInvalidLongitude),
		errors.Is(err, trip.ErrInvalidSosTriggeredBy),
		errors.Is(err, trip.ErrInvalidVehicleClass):
		return status.Error(codes.InvalidArgument, err.Error())

	case errors.Is(err, trip.ErrPickupOutsideServiceZone):
		return status.Error(codes.InvalidArgument, err.Error())

	default:
		h.logger.Error("unclassified trip request failure", "error", err)

		return status.Error(codes.Internal, "failed to process trip request")
	}
}

func toProtoStatus(s trip.Status) tripv1.TripStatus {
	switch s {
	case trip.StatusRequested:
		return tripv1.TripStatus_TRIP_STATUS_REQUESTED
	case trip.StatusAccepted:
		return tripv1.TripStatus_TRIP_STATUS_ACCEPTED
	case trip.StatusInProgress:
		return tripv1.TripStatus_TRIP_STATUS_IN_PROGRESS
	case trip.StatusCompleted:
		return tripv1.TripStatus_TRIP_STATUS_COMPLETED
	case trip.StatusCancelled:
		return tripv1.TripStatus_TRIP_STATUS_CANCELLED
	default:
		return tripv1.TripStatus_TRIP_STATUS_UNSPECIFIED
	}
}

func optionalTimestamp(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}

	return timestamppb.New(*t)
}

func toProtoTrip(t trip.Trip) *tripv1.Trip {
	return &tripv1.Trip{
		Id:       t.ID,
		RiderId:  t.RiderID,
		DriverId: t.DriverID,
		Status:   toProtoStatus(t.Status),
		Pickup: &tripv1.Coordinates{
			Latitude:  t.Pickup.Latitude,
			Longitude: t.Pickup.Longitude,
		},
		Dropoff: &tripv1.Coordinates{
			Latitude:  t.Dropoff.Latitude,
			Longitude: t.Dropoff.Longitude,
		},
		CancellationReason: t.CancellationReason,
		VehicleClass:       t.VehicleClass,
		RequestedAt:        timestamppb.New(t.RequestedAt),
		AcceptedAt:         optionalTimestamp(t.AcceptedAt),
		StartedAt:          optionalTimestamp(t.StartedAt),
		CompletedAt:        optionalTimestamp(t.CompletedAt),
		CancelledAt:        optionalTimestamp(t.CancelledAt),
	}
}

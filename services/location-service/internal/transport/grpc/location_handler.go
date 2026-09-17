package grpc

import (
	"context"
	"errors"
	"log/slog"

	locationv1 "github.com/7akoom/ride-platform/gen/go/ride/location/v1"
	"github.com/7akoom/ride-platform/services/location-service/internal/application/location"
	"github.com/7akoom/ride-platform/services/location-service/internal/application/zone"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type LocationHandler struct {
	locationv1.UnimplementedLocationServiceServer

	locationService location.Service
	zoneService     zone.Service
	logger          *slog.Logger
}

func NewLocationHandler(
	locationService location.Service,
	zoneService zone.Service,
	logger *slog.Logger,
) *LocationHandler {
	if locationService == nil {
		panic("location service is required")
	}

	if logger == nil {
		panic("logger is required")
	}

	if zoneService == nil {
		panic("zone service is required")
	}

	return &LocationHandler{
		locationService: locationService,
		zoneService:     zoneService,
	}
}

func (h *LocationHandler) UpdateLocation(
	ctx context.Context,
	request *locationv1.UpdateLocationRequest,
) (*locationv1.UpdateLocationResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	coordinates := request.GetCoordinates()

	updatedAt, err := h.locationService.UpdateLocation(
		ctx,
		location.UpdateLocationInput{
			EntityType: toDomainEntityType(request.GetEntityType()),
			EntityID:   request.GetEntityId(),
			Latitude:   coordinates.GetLatitude(),
			Longitude:  coordinates.GetLongitude(),
		},
	)
	if err != nil {
		return nil, h.mapLocationError(err)
	}

	return &locationv1.UpdateLocationResponse{
		UpdatedAt: timestamppb.New(updatedAt),
	}, nil
}

func (h *LocationHandler) GetLocation(
	ctx context.Context,
	request *locationv1.GetLocationRequest,
) (*locationv1.GetLocationResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	found, err := h.locationService.GetLocation(
		ctx,
		toDomainEntityType(request.GetEntityType()),
		request.GetEntityId(),
	)
	if err != nil {
		return nil, h.mapLocationError(err)
	}

	return &locationv1.GetLocationResponse{
		Coordinates: &locationv1.Coordinates{
			Latitude:  found.Coordinates.Latitude,
			Longitude: found.Coordinates.Longitude,
		},
		UpdatedAt: timestamppb.New(found.UpdatedAt),
	}, nil
}

func (h *LocationHandler) FindNearby(
	ctx context.Context,
	request *locationv1.FindNearbyRequest,
) (*locationv1.FindNearbyResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	coordinates := request.GetCoordinates()

	results, err := h.locationService.FindNearby(
		ctx,
		location.FindNearbyInput{
			EntityType:   toDomainEntityType(request.GetEntityType()),
			Latitude:     coordinates.GetLatitude(),
			Longitude:    coordinates.GetLongitude(),
			RadiusMeters: request.GetRadiusMeters(),
			Limit:        int(request.GetLimit()),
		},
	)
	if err != nil {
		return nil, h.mapLocationError(err)
	}

	protoEntities := make([]*locationv1.NearbyEntity, len(results))

	for i, entity := range results {
		protoEntities[i] = &locationv1.NearbyEntity{
			EntityId: entity.EntityID,
			Coordinates: &locationv1.Coordinates{
				Latitude:  entity.Coordinates.Latitude,
				Longitude: entity.Coordinates.Longitude,
			},
			DistanceMeters: entity.DistanceMeters,
		}
	}

	return &locationv1.FindNearbyResponse{
		Entities: protoEntities,
	}, nil
}

func (h *LocationHandler) mapLocationError(err error) error {
	switch {
	case errors.Is(err, location.ErrLocationNotFound):
		return status.Error(codes.NotFound, "location not found or stale")

	case errors.Is(err, location.ErrEntityIDRequired),
		errors.Is(err, location.ErrInvalidEntityType),
		errors.Is(err, location.ErrInvalidLatitude),
		errors.Is(err, location.ErrInvalidLongitude),
		errors.Is(err, location.ErrInvalidRadius):
		return status.Error(codes.InvalidArgument, err.Error())

	default:
		h.logger.Error("unclassified location request failure", "error", err)

		return status.Error(codes.Internal, "failed to process location request")
	}
}

func toDomainEntityType(e locationv1.EntityType) location.EntityType {
	switch e {
	case locationv1.EntityType_ENTITY_TYPE_DRIVER:
		return location.EntityDriver
	case locationv1.EntityType_ENTITY_TYPE_RIDER:
		return location.EntityRider
	default:
		return ""
	}
}

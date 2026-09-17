package grpc

import (
	"context"
	"errors"

	locationv1 "github.com/7akoom/ride-platform/gen/go/ride/location/v1"
	"github.com/7akoom/ride-platform/services/location-service/internal/application/zone"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (h *LocationHandler) CreateZone(
	ctx context.Context,
	request *locationv1.CreateZoneRequest,
) (*locationv1.ZoneResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	created, err := h.zoneService.CreateZone(ctx, zone.CreateZoneInput{
		City:     request.GetCity(),
		Name:     request.GetName(),
		Boundary: toDomainBoundary(request.GetBoundary()),
	})
	if err != nil {
		return nil, h.mapZoneError(err)
	}

	return &locationv1.ZoneResponse{Zone: toProtoZone(created)}, nil
}

func (h *LocationHandler) UpdateZone(
	ctx context.Context,
	request *locationv1.UpdateZoneRequest,
) (*locationv1.ZoneResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	updated, err := h.zoneService.UpdateZone(ctx, zone.UpdateZoneInput{
		ZoneID:   request.GetZoneId(),
		Name:     request.GetName(),
		Boundary: toDomainBoundary(request.GetBoundary()),
	})
	if err != nil {
		return nil, h.mapZoneError(err)
	}

	return &locationv1.ZoneResponse{Zone: toProtoZone(updated)}, nil
}

func (h *LocationHandler) SetZoneActive(
	ctx context.Context,
	request *locationv1.SetZoneActiveRequest,
) (*locationv1.ZoneResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	updated, err := h.zoneService.SetZoneActive(ctx, request.GetZoneId(), request.GetActive())
	if err != nil {
		return nil, h.mapZoneError(err)
	}

	return &locationv1.ZoneResponse{Zone: toProtoZone(updated)}, nil
}

func (h *LocationHandler) GetZone(
	ctx context.Context,
	request *locationv1.GetZoneRequest,
) (*locationv1.ZoneResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	found, err := h.zoneService.GetZone(ctx, request.GetZoneId())
	if err != nil {
		return nil, h.mapZoneError(err)
	}

	return &locationv1.ZoneResponse{Zone: toProtoZone(found)}, nil
}

func (h *LocationHandler) ListZones(
	ctx context.Context,
	request *locationv1.ListZonesRequest,
) (*locationv1.ListZonesResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	results, err := h.zoneService.ListZones(ctx, request.GetCity())
	if err != nil {
		return nil, h.mapZoneError(err)
	}

	protoZones := make([]*locationv1.Zone, len(results))
	for i, z := range results {
		protoZones[i] = toProtoZone(z)
	}

	return &locationv1.ListZonesResponse{Zones: protoZones}, nil
}

func (h *LocationHandler) CheckServiceZone(
	ctx context.Context,
	request *locationv1.CheckServiceZoneRequest,
) (*locationv1.CheckServiceZoneResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	coordinates := request.GetCoordinates()

	result, err := h.zoneService.CheckServiceZone(
		ctx,
		coordinates.GetLatitude(),
		coordinates.GetLongitude(),
	)
	if err != nil {
		return nil, h.mapZoneError(err)
	}

	return &locationv1.CheckServiceZoneResponse{
		Served: result.Served,
		ZoneId: result.ZoneID,
		City:   result.City,
	}, nil
}

func toDomainBoundary(points []*locationv1.Coordinates) []zone.Coordinates {
	boundary := make([]zone.Coordinates, len(points))

	for i, point := range points {
		boundary[i] = zone.Coordinates{
			Latitude:  point.GetLatitude(),
			Longitude: point.GetLongitude(),
		}
	}

	return boundary
}

func toProtoZone(z zone.Zone) *locationv1.Zone {
	boundary := make([]*locationv1.Coordinates, len(z.Boundary))

	for i, point := range z.Boundary {
		boundary[i] = &locationv1.Coordinates{
			Latitude:  point.Latitude,
			Longitude: point.Longitude,
		}
	}

	return &locationv1.Zone{
		Id:        z.ID,
		City:      z.City,
		Name:      z.Name,
		Boundary:  boundary,
		Active:    z.Active,
		CreatedAt: timestamppb.New(z.CreatedAt),
		UpdatedAt: timestamppb.New(z.UpdatedAt),
	}
}

func (h *LocationHandler) mapZoneError(err error) error {
	switch {
	case errors.Is(err, zone.ErrZoneNotFound):
		return status.Error(codes.NotFound, "zone not found")

	case errors.Is(err, zone.ErrCityRequired),
		errors.Is(err, zone.ErrNameRequired),
		errors.Is(err, zone.ErrZoneIDRequired),
		errors.Is(err, zone.ErrBoundaryTooFewPoints),
		errors.Is(err, zone.ErrInvalidLatitude),
		errors.Is(err, zone.ErrInvalidLongitude):
		return status.Error(codes.InvalidArgument, err.Error())

	default:
		h.logger.Error("unclassified zone request failure", "error", err)

		return status.Error(codes.Internal, "failed to process zone request")
	}
}

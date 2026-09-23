package grpc

import (
	"context"
	"errors"

	locationv1 "github.com/7akoom/ride-platform/gen/go/ride/location/v1"
	"github.com/7akoom/ride-platform/services/location-service/internal/application/maps"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// mapsService is what the map RPCs need: maps.Service.
type mapsService interface {
	GetRoute(ctx context.Context, input maps.RouteInput) (maps.Route, error)
	SearchPlaces(ctx context.Context, input maps.SearchInput) ([]maps.Place, error)
	ReverseGeocode(ctx context.Context, input maps.ReverseInput) (maps.Place, error)
}

// WithMaps makes the handler serve routes, place search and reverse geocoding. Until
// it is called those RPCs answer UNIMPLEMENTED, so building a handler as before
// (NewLocationHandler) keeps working unchanged.
func (h *LocationHandler) WithMaps(service mapsService) *LocationHandler {
	if service == nil {
		panic("maps service is required")
	}

	h.mapService = service

	return h
}

// GetRoute returns the best route by road between two points: how far, how long, and
// the line to draw. Any signed-in user may ask.
func (h *LocationHandler) GetRoute(
	ctx context.Context,
	request *locationv1.GetRouteRequest,
) (*locationv1.GetRouteResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	if h.mapService == nil {
		return nil, status.Error(codes.Unimplemented, "maps are not configured")
	}

	route, err := h.mapService.GetRoute(ctx, maps.RouteInput{
		Origin:      toMapsPoint(request.GetOrigin()),
		Destination: toMapsPoint(request.GetDestination()),
	})
	if err != nil {
		return nil, h.mapMapsError(err)
	}

	return &locationv1.GetRouteResponse{
		DistanceMeters:  route.DistanceMeters,
		DurationSeconds: route.DurationSeconds,
		Polyline:        route.Polyline,
	}, nil
}

// SearchPlaces finds places by name. Any signed-in user may ask.
func (h *LocationHandler) SearchPlaces(
	ctx context.Context,
	request *locationv1.SearchPlacesRequest,
) (*locationv1.SearchPlacesResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	if h.mapService == nil {
		return nil, status.Error(codes.Unimplemented, "maps are not configured")
	}

	found, err := h.mapService.SearchPlaces(ctx, maps.SearchInput{
		Query:    request.GetQuery(),
		Near:     toMapsPoint(request.GetNear()),
		Limit:    int(request.GetLimit()),
		Language: request.GetLanguage(),
	})
	if err != nil {
		return nil, h.mapMapsError(err)
	}

	places := make([]*locationv1.Place, 0, len(found))
	for _, place := range found {
		places = append(places, toProtoMapPlace(place))
	}

	return &locationv1.SearchPlacesResponse{Places: places}, nil
}

// ReverseGeocode names what is at a point. Any signed-in user may ask.
func (h *LocationHandler) ReverseGeocode(
	ctx context.Context,
	request *locationv1.ReverseGeocodeRequest,
) (*locationv1.ReverseGeocodeResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	if h.mapService == nil {
		return nil, status.Error(codes.Unimplemented, "maps are not configured")
	}

	place, err := h.mapService.ReverseGeocode(ctx, maps.ReverseInput{
		Coordinates: toMapsPoint(request.GetCoordinates()),
		Language:    request.GetLanguage(),
	})
	if err != nil {
		return nil, h.mapMapsError(err)
	}

	return &locationv1.ReverseGeocodeResponse{Place: toProtoMapPlace(place)}, nil
}

// mapMapsError gives each refusal its own code: a bad request is the caller's to fix
// (INVALID_ARGUMENT), "no route" and "nothing there" are answers (NOT_FOUND), a point
// off the road network is a precondition (FAILED_PRECONDITION), and a map server that
// is down is UNAVAILABLE, which an app can retry.
func (h *LocationHandler) mapMapsError(err error) error {
	switch {
	case errors.Is(err, maps.ErrPointRequired),
		errors.Is(err, maps.ErrInvalidLatitude),
		errors.Is(err, maps.ErrInvalidLongitude),
		errors.Is(err, maps.ErrQueryTooShort),
		errors.Is(err, maps.ErrQueryTooLong),
		errors.Is(err, maps.ErrInvalidLimit),
		errors.Is(err, maps.ErrInvalidLanguage):
		return status.Error(codes.InvalidArgument, err.Error())

	case errors.Is(err, maps.ErrNoRoute), errors.Is(err, maps.ErrPlaceNotFound):
		return status.Error(codes.NotFound, err.Error())

	case errors.Is(err, maps.ErrNotNearRoad):
		return status.Error(codes.FailedPrecondition, err.Error())

	case errors.Is(err, maps.ErrUnavailable):
		h.logger.Warn("the map service is not available", "error", err)

		return status.Error(codes.Unavailable, "the map service is not available, try again shortly")

	default:
		h.logger.Error("unclassified map request failure", "error", err)

		return status.Error(codes.Internal, "failed to process map request")
	}
}

func toMapsPoint(c *locationv1.Coordinates) *maps.Coordinates {
	if c == nil {
		return nil
	}

	return &maps.Coordinates{Latitude: c.GetLatitude(), Longitude: c.GetLongitude()}
}

func toProtoMapPlace(place maps.Place) *locationv1.Place {
	return &locationv1.Place{
		Id:          place.ID,
		Name:        place.Name,
		DisplayName: place.DisplayName,
		Category:    place.Category,
		Type:        place.Type,
		Coordinates: &locationv1.Coordinates{Latitude: place.Coordinates.Latitude, Longitude: place.Coordinates.Longitude},
		Address:     place.Address,

		CuratedPlaceId: place.CuratedPlaceID,
	}
}

package grpc

import (
	"context"
	"errors"
	"strings"

	locationv1 "github.com/7akoom/ride-platform/gen/go/ride/location/v1"
	"github.com/7akoom/ride-platform/services/location-service/internal/application/city"
	"github.com/7akoom/ride-platform/services/location-service/internal/application/localized"
	"github.com/7akoom/ride-platform/services/location-service/internal/application/place"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// WithCatalog makes the handler serve cities and curated places. Until it is
// called those RPCs answer UNIMPLEMENTED.
func (h *LocationHandler) WithCatalog(cities *city.Service, places *place.Service) *LocationHandler {
	if cities == nil || places == nil {
		panic("city and place services are required")
	}

	h.cities = cities
	h.places = places

	return h
}

func (h *LocationHandler) catalogReady() error {
	if h.cities == nil || h.places == nil {
		return status.Error(codes.Unimplemented, "cities and places are not configured")
	}

	return nil
}

// --- cities ---------------------------------------------------------------

func (h *LocationHandler) ListCities(ctx context.Context, _ *locationv1.ListCitiesRequest) (*locationv1.ListCitiesResponse, error) {
	return h.listCities(ctx, false)
}

func (h *LocationHandler) AdminListCities(ctx context.Context, _ *locationv1.AdminListCitiesRequest) (*locationv1.ListCitiesResponse, error) {
	return h.listCities(ctx, true)
}

func (h *LocationHandler) listCities(ctx context.Context, includeInactive bool) (*locationv1.ListCitiesResponse, error) {
	if err := h.catalogReady(); err != nil {
		return nil, err
	}

	cities, err := h.cities.List(ctx, includeInactive)
	if err != nil {
		return nil, h.mapCatalogError(err)
	}

	out := make([]*locationv1.City, 0, len(cities))
	for _, c := range cities {
		out = append(out, toProtoCity(c))
	}

	return &locationv1.ListCitiesResponse{Cities: out}, nil
}

func (h *LocationHandler) GetCity(ctx context.Context, request *locationv1.GetCityRequest) (*locationv1.CityResponse, error) {
	if err := h.catalogReady(); err != nil {
		return nil, err
	}

	found, err := h.cities.Get(ctx, request.GetCityId(), isInternalCall(ctx))
	if err != nil {
		return nil, h.mapCatalogError(err)
	}

	return &locationv1.CityResponse{City: toProtoCity(found)}, nil
}

func (h *LocationHandler) CreateCity(ctx context.Context, request *locationv1.CreateCityRequest) (*locationv1.CityResponse, error) {
	if err := h.catalogReady(); err != nil {
		return nil, err
	}

	created, err := h.cities.Create(ctx, city.Details{
		Name:     request.GetName(),
		Names:    request.GetNames(),
		TimeZone: request.GetTimeZone(),
		Center:   toCityPoint(request.GetCenter()),
	})
	if err != nil {
		return nil, h.mapCatalogError(err)
	}

	return &locationv1.CityResponse{City: toProtoCity(created)}, nil
}

func (h *LocationHandler) UpdateCity(ctx context.Context, request *locationv1.UpdateCityRequest) (*locationv1.CityResponse, error) {
	if err := h.catalogReady(); err != nil {
		return nil, err
	}

	updated, err := h.cities.Update(ctx, request.GetCityId(), city.Details{
		Name:     request.GetName(),
		Names:    request.GetNames(),
		TimeZone: request.GetTimeZone(),
		Center:   toCityPoint(request.GetCenter()),
	})
	if err != nil {
		return nil, h.mapCatalogError(err)
	}

	return &locationv1.CityResponse{City: toProtoCity(updated)}, nil
}

func (h *LocationHandler) SetCityActive(ctx context.Context, request *locationv1.SetCityActiveRequest) (*locationv1.CityResponse, error) {
	if err := h.catalogReady(); err != nil {
		return nil, err
	}

	updated, err := h.cities.SetActive(ctx, request.GetCityId(), request.GetActive())
	if err != nil {
		return nil, h.mapCatalogError(err)
	}

	return &locationv1.CityResponse{City: toProtoCity(updated)}, nil
}

// --- curated places ---------------------------------------------------------

func (h *LocationHandler) ListPlaces(ctx context.Context, request *locationv1.ListPlacesRequest) (*locationv1.ListPlacesResponse, error) {
	return h.listPlaces(ctx, place.ListInput{
		CityID:    request.GetCityId(),
		Near:      toPlacePointOrNil(request.GetNear()),
		PageSize:  int(request.GetPageSize()),
		PageToken: request.GetPageToken(),
	}, request.GetCategory())
}

func (h *LocationHandler) AdminListPlaces(ctx context.Context, request *locationv1.AdminListPlacesRequest) (*locationv1.ListPlacesResponse, error) {
	return h.listPlaces(ctx, place.ListInput{
		CityID:          request.GetCityId(),
		IncludeInactive: true,
		PageSize:        int(request.GetPageSize()),
		PageToken:       request.GetPageToken(),
	}, request.GetCategory())
}

func (h *LocationHandler) listPlaces(
	ctx context.Context,
	input place.ListInput,
	category locationv1.PlaceCategory,
) (*locationv1.ListPlacesResponse, error) {
	if err := h.catalogReady(); err != nil {
		return nil, err
	}

	if category != locationv1.PlaceCategory_PLACE_CATEGORY_UNSPECIFIED {
		domain, ok := categoryFromProto(category)
		if !ok {
			return nil, status.Error(codes.InvalidArgument, place.ErrInvalidCategory.Error())
		}

		input.Category = domain
	}

	places, next, err := h.places.List(ctx, input)
	if err != nil {
		return nil, h.mapCatalogError(err)
	}

	out := make([]*locationv1.CuratedPlace, 0, len(places))
	for _, p := range places {
		out = append(out, toProtoCuratedPlace(p))
	}

	return &locationv1.ListPlacesResponse{Places: out, NextPageToken: next}, nil
}

func (h *LocationHandler) GetPlace(ctx context.Context, request *locationv1.GetPlaceRequest) (*locationv1.CuratedPlaceResponse, error) {
	if err := h.catalogReady(); err != nil {
		return nil, err
	}

	found, err := h.places.Get(ctx, request.GetPlaceId(), isInternalCall(ctx))
	if err != nil {
		return nil, h.mapCatalogError(err)
	}

	return &locationv1.CuratedPlaceResponse{Place: toProtoCuratedPlace(found)}, nil
}

func (h *LocationHandler) CreatePlace(ctx context.Context, request *locationv1.CreatePlaceRequest) (*locationv1.CuratedPlaceResponse, error) {
	if err := h.catalogReady(); err != nil {
		return nil, err
	}

	details, err := placeDetails(request.GetCategory(), request.GetName(), request.GetNames(),
		request.GetAddress(), request.GetCoordinates(), request.GetPriority())
	if err != nil {
		return nil, err
	}

	created, err := h.places.Create(ctx, request.GetCityId(), details)
	if err != nil {
		return nil, h.mapCatalogError(err)
	}

	return &locationv1.CuratedPlaceResponse{Place: toProtoCuratedPlace(created)}, nil
}

func (h *LocationHandler) UpdatePlace(ctx context.Context, request *locationv1.UpdatePlaceRequest) (*locationv1.CuratedPlaceResponse, error) {
	if err := h.catalogReady(); err != nil {
		return nil, err
	}

	details, err := placeDetails(request.GetCategory(), request.GetName(), request.GetNames(),
		request.GetAddress(), request.GetCoordinates(), request.GetPriority())
	if err != nil {
		return nil, err
	}

	updated, err := h.places.Update(ctx, request.GetPlaceId(), details)
	if err != nil {
		return nil, h.mapCatalogError(err)
	}

	return &locationv1.CuratedPlaceResponse{Place: toProtoCuratedPlace(updated)}, nil
}

func (h *LocationHandler) SetPlaceActive(ctx context.Context, request *locationv1.SetPlaceActiveRequest) (*locationv1.CuratedPlaceResponse, error) {
	if err := h.catalogReady(); err != nil {
		return nil, err
	}

	updated, err := h.places.SetActive(ctx, request.GetPlaceId(), request.GetActive())
	if err != nil {
		return nil, h.mapCatalogError(err)
	}

	return &locationv1.CuratedPlaceResponse{Place: toProtoCuratedPlace(updated)}, nil
}

func placeDetails(
	category locationv1.PlaceCategory,
	name string,
	names map[string]string,
	address string,
	coordinates *locationv1.Coordinates,
	priority int32,
) (place.Details, error) {
	domain, ok := categoryFromProto(category)
	if !ok {
		return place.Details{}, status.Error(codes.InvalidArgument, place.ErrInvalidCategory.Error())
	}

	point := place.Coordinates{}
	if coordinates != nil {
		point = place.Coordinates{Latitude: coordinates.GetLatitude(), Longitude: coordinates.GetLongitude()}
	}

	return place.Details{
		Category:    domain,
		Name:        name,
		Names:       names,
		Address:     address,
		Coordinates: point,
		Priority:    int(priority),
	}, nil
}

// --- errors and conversions -------------------------------------------------

func (h *LocationHandler) mapCatalogError(err error) error {
	switch {
	case errors.Is(err, city.ErrCityNotFound), errors.Is(err, place.ErrCityNotFound):
		return status.Error(codes.NotFound, "city not found")

	case errors.Is(err, place.ErrPlaceNotFound):
		return status.Error(codes.NotFound, "place not found")

	case errors.Is(err, city.ErrCityNameTaken):
		return status.Error(codes.AlreadyExists, err.Error())

	case errors.Is(err, city.ErrCityIDRequired),
		errors.Is(err, city.ErrTimeZoneRequired),
		errors.Is(err, city.ErrUnknownTimeZone),
		errors.Is(err, city.ErrCenterRequired),
		errors.Is(err, city.ErrInvalidLatitude),
		errors.Is(err, city.ErrInvalidLongitude),
		errors.Is(err, place.ErrPlaceIDRequired),
		errors.Is(err, place.ErrCityRequired),
		errors.Is(err, place.ErrInvalidCategory),
		errors.Is(err, place.ErrAddressTooLong),
		errors.Is(err, place.ErrPointRequired),
		errors.Is(err, place.ErrInvalidLatitude),
		errors.Is(err, place.ErrInvalidLongitude),
		errors.Is(err, place.ErrInvalidPriority),
		errors.Is(err, place.ErrInvalidPageSize),
		errors.Is(err, place.ErrInvalidPageToken),
		errors.Is(err, localized.ErrNameRequired),
		errors.Is(err, localized.ErrNameTooLong),
		errors.Is(err, localized.ErrUnknownLanguage):
		return status.Error(codes.InvalidArgument, err.Error())

	default:
		h.logger.Error("unclassified city or place request failure", "error", err)

		return status.Error(codes.Internal, "failed to process the request")
	}
}

var categoriesFromProto = map[locationv1.PlaceCategory]place.Category{
	locationv1.PlaceCategory_PLACE_CATEGORY_AIRPORT:    place.CategoryAirport,
	locationv1.PlaceCategory_PLACE_CATEGORY_MALL:       place.CategoryMall,
	locationv1.PlaceCategory_PLACE_CATEGORY_HOTEL:      place.CategoryHotel,
	locationv1.PlaceCategory_PLACE_CATEGORY_HOSPITAL:   place.CategoryHospital,
	locationv1.PlaceCategory_PLACE_CATEGORY_UNIVERSITY: place.CategoryUniversity,
	locationv1.PlaceCategory_PLACE_CATEGORY_LANDMARK:   place.CategoryLandmark,
	locationv1.PlaceCategory_PLACE_CATEGORY_STATION:    place.CategoryStation,
	locationv1.PlaceCategory_PLACE_CATEGORY_GOVERNMENT: place.CategoryGovernment,
	locationv1.PlaceCategory_PLACE_CATEGORY_RESTAURANT: place.CategoryRestaurant,
	locationv1.PlaceCategory_PLACE_CATEGORY_OTHER:      place.CategoryOther,
}

func categoryFromProto(category locationv1.PlaceCategory) (place.Category, bool) {
	found, ok := categoriesFromProto[category]

	return found, ok
}

func categoryToProto(category place.Category) locationv1.PlaceCategory {
	for protoCategory, domain := range categoriesFromProto {
		if domain == category {
			return protoCategory
		}
	}

	return locationv1.PlaceCategory_PLACE_CATEGORY_UNSPECIFIED
}

func toCityPoint(c *locationv1.Coordinates) city.Coordinates {
	if c == nil {
		return city.Coordinates{}
	}

	return city.Coordinates{Latitude: c.GetLatitude(), Longitude: c.GetLongitude()}
}

func toPlacePointOrNil(c *locationv1.Coordinates) *place.Coordinates {
	if c == nil {
		return nil
	}

	return &place.Coordinates{Latitude: c.GetLatitude(), Longitude: c.GetLongitude()}
}

func toProtoCity(c city.City) *locationv1.City {
	return &locationv1.City{
		Id:        c.ID,
		Name:      c.Name,
		Names:     c.Names,
		TimeZone:  c.TimeZone,
		Center:    &locationv1.Coordinates{Latitude: c.Center.Latitude, Longitude: c.Center.Longitude},
		Active:    c.Active,
		CreatedAt: timestamppb.New(c.CreatedAt),
		UpdatedAt: timestamppb.New(c.UpdatedAt),
	}
}

func toProtoCuratedPlace(p place.Place) *locationv1.CuratedPlace {
	return &locationv1.CuratedPlace{
		Id:          p.ID,
		CityId:      p.CityID,
		Category:    categoryToProto(p.Category),
		Name:        p.Name,
		Names:       p.Names,
		Address:     p.Address,
		Coordinates: &locationv1.Coordinates{Latitude: p.Coordinates.Latitude, Longitude: p.Coordinates.Longitude},
		Priority:    int32(p.Priority),
		Active:      p.Active,
		CreatedAt:   timestamppb.New(p.CreatedAt),
		UpdatedAt:   timestamppb.New(p.UpdatedAt),
	}
}

// isInternalCall reports whether the caller is another service (internal
// token), which may read inactive cities and places.
func isInternalCall(ctx context.Context) bool {
	principal, ok := authenticatedPrincipalFromContext(ctx)

	return ok && strings.EqualFold(principal.IdentityID, internalServicePrincipalID)
}

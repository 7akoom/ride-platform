package place

import (
	"context"
	"encoding/base64"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/7akoom/ride-platform/services/location-service/internal/application/localized"
)

type Service struct {
	repository  Repository
	idGenerator IDGenerator
}

func NewService(repository Repository, idGenerator IDGenerator) *Service {
	if repository == nil || idGenerator == nil {
		panic("place service dependencies are required")
	}

	return &Service{repository: repository, idGenerator: idGenerator}
}

var uuidShape = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func checkID(id string, required, notFound error) (string, error) {
	id = strings.ToLower(strings.TrimSpace(id))

	switch {
	case id == "":
		return "", required
	case !uuidShape.MatchString(id):
		return "", notFound
	}

	return id, nil
}

func (s *Service) Create(ctx context.Context, cityID string, details Details) (Place, error) {
	cityID, err := checkID(cityID, ErrCityRequired, ErrCityNotFound)
	if err != nil {
		return Place{}, err
	}

	clean, err := validate(details)
	if err != nil {
		return Place{}, err
	}

	created, err := s.repository.Create(ctx, s.idGenerator.NewID(), cityID, clean)
	if err != nil {
		return Place{}, fmt.Errorf("create place: %w", err)
	}

	return created, nil
}

func (s *Service) Update(ctx context.Context, id string, details Details) (Place, error) {
	id, err := checkID(id, ErrPlaceIDRequired, ErrPlaceNotFound)
	if err != nil {
		return Place{}, err
	}

	clean, err := validate(details)
	if err != nil {
		return Place{}, err
	}

	updated, err := s.repository.Update(ctx, id, clean)
	if err != nil {
		return Place{}, fmt.Errorf("update place: %w", err)
	}

	return updated, nil
}

func (s *Service) SetActive(ctx context.Context, id string, active bool) (Place, error) {
	id, err := checkID(id, ErrPlaceIDRequired, ErrPlaceNotFound)
	if err != nil {
		return Place{}, err
	}

	updated, err := s.repository.SetActive(ctx, id, active)
	if err != nil {
		return Place{}, fmt.Errorf("set place active: %w", err)
	}

	return updated, nil
}

// Get returns a place. Unless includeInactive, an inactive place is not found.
func (s *Service) Get(ctx context.Context, id string, includeInactive bool) (Place, error) {
	id, err := checkID(id, ErrPlaceIDRequired, ErrPlaceNotFound)
	if err != nil {
		return Place{}, err
	}

	found, err := s.repository.Get(ctx, id)
	if err != nil {
		return Place{}, fmt.Errorf("get place: %w", err)
	}

	if !found.Active && !includeInactive {
		return Place{}, ErrPlaceNotFound
	}

	return found, nil
}

// ListInput asks for one page.
type ListInput struct {
	CityID          string
	Category        Category
	Near            *Coordinates
	IncludeInactive bool
	PageSize        int
	PageToken       string
}

// List returns one page of places and the token of the next one ("" on the
// last page).
func (s *Service) List(ctx context.Context, input ListInput) ([]Place, string, error) {
	filter := Filter{Category: input.Category, IncludeInactive: input.IncludeInactive}

	if strings.TrimSpace(input.CityID) != "" {
		cityID, err := checkID(input.CityID, ErrCityRequired, ErrCityNotFound)
		if err != nil {
			return nil, "", err
		}

		filter.CityID = cityID
	}

	if filter.Category != "" && !filter.Category.Valid() {
		return nil, "", ErrInvalidCategory
	}

	if input.Near != nil {
		if err := input.Near.validate(); err != nil {
			return nil, "", err
		}

		filter.Near = input.Near
	}

	switch {
	case input.PageSize < 0:
		return nil, "", ErrInvalidPageSize
	case input.PageSize == 0:
		filter.Limit = DefaultPageSize
	case input.PageSize > MaxPageSize:
		filter.Limit = MaxPageSize
	default:
		filter.Limit = input.PageSize
	}

	offset, err := decodePageToken(input.PageToken)
	if err != nil {
		return nil, "", err
	}

	filter.Offset = offset

	places, err := s.repository.List(ctx, filter)
	if err != nil {
		return nil, "", fmt.Errorf("list places: %w", err)
	}

	next := ""
	if len(places) > filter.Limit {
		places = places[:filter.Limit]
		next = encodePageToken(offset + filter.Limit)
	}

	return places, next, nil
}

// Search finds curated places for the map search (see maps.CuratedSearcher).
func (s *Service) Search(ctx context.Context, query string, near *Coordinates, limit int) ([]Match, error) {
	query = strings.TrimSpace(query)
	if query == "" || limit <= 0 {
		return nil, nil
	}

	matches, err := s.repository.Search(ctx, query, near, limit)
	if err != nil {
		return nil, fmt.Errorf("search places: %w", err)
	}

	return matches, nil
}

func validate(details Details) (Details, error) {
	if !details.Category.Valid() {
		return Details{}, ErrInvalidCategory
	}

	name, err := localized.Name(details.Name)
	if err != nil {
		return Details{}, err
	}

	names, err := localized.Names(details.Names)
	if err != nil {
		return Details{}, err
	}

	address := strings.TrimSpace(details.Address)
	if utf8.RuneCountInString(address) > MaxAddressLength {
		return Details{}, ErrAddressTooLong
	}

	if details.Coordinates == (Coordinates{}) {
		return Details{}, ErrPointRequired
	}

	if err := details.Coordinates.validate(); err != nil {
		return Details{}, err
	}

	if details.Priority < MinPriority || details.Priority > MaxPriority {
		return Details{}, ErrInvalidPriority
	}

	return Details{
		Category:    details.Category,
		Name:        name,
		Names:       names,
		Address:     address,
		Coordinates: details.Coordinates,
		Priority:    details.Priority,
	}, nil
}

const maxOffset = 100_000

func encodePageToken(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte("o:" + strconv.Itoa(offset)))
}

func decodePageToken(token string) (int, error) {
	if token == "" {
		return 0, nil
	}

	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return 0, ErrInvalidPageToken
	}

	value, found := strings.CutPrefix(string(raw), "o:")
	if !found {
		return 0, ErrInvalidPageToken
	}

	offset, err := strconv.Atoi(value)
	if err != nil || offset < 0 || offset > maxOffset {
		return 0, ErrInvalidPageToken
	}

	return offset, nil
}

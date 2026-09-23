package city

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/7akoom/ride-platform/services/location-service/internal/application/localized"
)

type Service struct {
	repository  Repository
	idGenerator IDGenerator
}

func NewService(repository Repository, idGenerator IDGenerator) *Service {
	if repository == nil || idGenerator == nil {
		panic("city service dependencies are required")
	}

	return &Service{repository: repository, idGenerator: idGenerator}
}

var uuidShape = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// CheckID validates the shape of a city id, so a malformed one answers "not
// found" without reaching the database.
func CheckID(id string) (string, error) {
	id = strings.ToLower(strings.TrimSpace(id))

	switch {
	case id == "":
		return "", ErrCityIDRequired
	case !uuidShape.MatchString(id):
		return "", ErrCityNotFound
	}

	return id, nil
}

func (s *Service) Create(ctx context.Context, details Details) (City, error) {
	clean, err := validate(details)
	if err != nil {
		return City{}, err
	}

	created, err := s.repository.Create(ctx, s.idGenerator.NewID(), clean)
	if err != nil {
		return City{}, fmt.Errorf("create city: %w", err)
	}

	return created, nil
}

func (s *Service) Update(ctx context.Context, id string, details Details) (City, error) {
	id, err := CheckID(id)
	if err != nil {
		return City{}, err
	}

	clean, err := validate(details)
	if err != nil {
		return City{}, err
	}

	updated, err := s.repository.Update(ctx, id, clean)
	if err != nil {
		return City{}, fmt.Errorf("update city: %w", err)
	}

	return updated, nil
}

func (s *Service) SetActive(ctx context.Context, id string, active bool) (City, error) {
	id, err := CheckID(id)
	if err != nil {
		return City{}, err
	}

	updated, err := s.repository.SetActive(ctx, id, active)
	if err != nil {
		return City{}, fmt.Errorf("set city active: %w", err)
	}

	return updated, nil
}

// Get returns a city. Unless includeInactive, an inactive city is not found.
func (s *Service) Get(ctx context.Context, id string, includeInactive bool) (City, error) {
	id, err := CheckID(id)
	if err != nil {
		return City{}, err
	}

	found, err := s.repository.Get(ctx, id)
	if err != nil {
		return City{}, fmt.Errorf("get city: %w", err)
	}

	if !found.Active && !includeInactive {
		return City{}, ErrCityNotFound
	}

	return found, nil
}

func (s *Service) List(ctx context.Context, includeInactive bool) ([]City, error) {
	cities, err := s.repository.List(ctx, includeInactive)
	if err != nil {
		return nil, fmt.Errorf("list cities: %w", err)
	}

	return cities, nil
}

func validate(details Details) (Details, error) {
	name, err := localized.Name(details.Name)
	if err != nil {
		return Details{}, err
	}

	names, err := localized.Names(details.Names)
	if err != nil {
		return Details{}, err
	}

	zoneName, err := timeZone(details.TimeZone)
	if err != nil {
		return Details{}, err
	}

	if details.Center == (Coordinates{}) {
		return Details{}, ErrCenterRequired
	}

	if err := details.Center.validate(); err != nil {
		return Details{}, err
	}

	return Details{Name: name, Names: names, TimeZone: zoneName, Center: details.Center}, nil
}

// timeZone accepts IANA names only: "Local" and "UTC" offsets written by hand
// would silently shift every schedule.
func timeZone(raw string) (string, error) {
	name := strings.TrimSpace(raw)

	switch {
	case name == "":
		return "", ErrTimeZoneRequired
	case name == "Local" || strings.HasPrefix(name, "/") || strings.Contains(name, ".."):
		return "", ErrUnknownTimeZone
	}

	if _, err := time.LoadLocation(name); err != nil {
		return "", ErrUnknownTimeZone
	}

	return name, nil
}

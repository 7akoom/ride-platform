// Package city holds the cities the platform operates in. A deployment serves
// one country in one currency; a city adds its own time zone, the point a map
// of it opens at, and the zones and curated places inside it.
package city

import (
	"context"
	"errors"
	"time"
)

var (
	ErrCityIDRequired   = errors.New("city id is required")
	ErrCityNotFound     = errors.New("city not found")
	ErrCityNameTaken    = errors.New("a city with this name already exists")
	ErrTimeZoneRequired = errors.New("time zone is required")
	ErrUnknownTimeZone  = errors.New("time zone is not a known IANA zone (for example Asia/Baghdad)")
	ErrCenterRequired   = errors.New("center is required")
	ErrInvalidLatitude  = errors.New("latitude must be between -90 and 90")
	ErrInvalidLongitude = errors.New("longitude must be between -180 and 180")
)

type Coordinates struct {
	Latitude  float64
	Longitude float64
}

func (c Coordinates) validate() error {
	if c.Latitude < -90 || c.Latitude > 90 {
		return ErrInvalidLatitude
	}

	if c.Longitude < -180 || c.Longitude > 180 {
		return ErrInvalidLongitude
	}

	return nil
}

type City struct {
	ID        string
	Name      string
	Names     map[string]string
	TimeZone  string
	Center    Coordinates
	Active    bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Details is what staff set on a city.
type Details struct {
	Name     string
	Names    map[string]string
	TimeZone string
	Center   Coordinates
}

// Repository persists cities. Create and Update return ErrCityNameTaken when
// another city has the same name (ignoring case); Update and SetActive return
// ErrCityNotFound for an unknown id.
type Repository interface {
	Create(ctx context.Context, id string, details Details) (City, error)
	Update(ctx context.Context, id string, details Details) (City, error)
	SetActive(ctx context.Context, id string, active bool) (City, error)
	Get(ctx context.Context, id string) (City, error)
	// List returns cities by name; inactive ones only when asked.
	List(ctx context.Context, includeInactive bool) ([]City, error)
}

type IDGenerator interface {
	NewID() string
}

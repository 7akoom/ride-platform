// Package place holds curated places: the airports, malls, hotels... that staff
// chose, named in every language, each with the exact point to be picked up or
// dropped at. They come first in search, before places from the map.
package place

import (
	"context"
	"errors"
	"time"
)

type Category string

const (
	CategoryAirport    Category = "airport"
	CategoryMall       Category = "mall"
	CategoryHotel      Category = "hotel"
	CategoryHospital   Category = "hospital"
	CategoryUniversity Category = "university"
	CategoryLandmark   Category = "landmark"
	CategoryStation    Category = "station"
	CategoryGovernment Category = "government"
	CategoryRestaurant Category = "restaurant"
	CategoryOther      Category = "other"
)

// Categories lists every category, in the order the proto enum numbers them.
var Categories = []Category{
	CategoryAirport, CategoryMall, CategoryHotel, CategoryHospital, CategoryUniversity,
	CategoryLandmark, CategoryStation, CategoryGovernment, CategoryRestaurant, CategoryOther,
}

func (c Category) Valid() bool {
	for _, known := range Categories {
		if c == known {
			return true
		}
	}

	return false
}

const (
	MaxAddressLength = 300
	MinPriority      = -1000
	MaxPriority      = 1000
	DefaultPageSize  = 20
	MaxPageSize      = 100
)

var (
	ErrPlaceIDRequired  = errors.New("place id is required")
	ErrPlaceNotFound    = errors.New("place not found")
	ErrCityRequired     = errors.New("city id is required")
	ErrCityNotFound     = errors.New("city not found")
	ErrInvalidCategory  = errors.New("category is not valid")
	ErrAddressTooLong   = errors.New("address is longer than 300 characters")
	ErrPointRequired    = errors.New("coordinates are required")
	ErrInvalidLatitude  = errors.New("latitude must be between -90 and 90")
	ErrInvalidLongitude = errors.New("longitude must be between -180 and 180")
	ErrInvalidPriority  = errors.New("priority must be between -1000 and 1000")
	ErrInvalidPageSize  = errors.New("page size must not be negative")
	ErrInvalidPageToken = errors.New("page token is not valid")
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

type Place struct {
	ID          string
	CityID      string
	CityName    string
	CityNames   map[string]string
	Category    Category
	Name        string
	Names       map[string]string
	Address     string
	Coordinates Coordinates
	Priority    int
	Active      bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Details is what staff set on a place.
type Details struct {
	Category    Category
	Name        string
	Names       map[string]string
	Address     string
	Coordinates Coordinates
	Priority    int
}

// Filter narrows a list. Near orders closer places first (after priority).
type Filter struct {
	CityID          string
	Category        Category
	Near            *Coordinates
	IncludeInactive bool
	Offset          int
	Limit           int
}

// Match is a curated place found by a search, with how far it is from the
// point the search was ranked towards (0 without one).
type Match struct {
	Place          Place
	DistanceMeters float64
}

// Repository persists places. Create returns ErrCityNotFound for an unknown
// city; Update and SetActive return ErrPlaceNotFound for an unknown place.
type Repository interface {
	Create(ctx context.Context, id string, cityID string, details Details) (Place, error)
	Update(ctx context.Context, id string, details Details) (Place, error)
	SetActive(ctx context.Context, id string, active bool) (Place, error)
	Get(ctx context.Context, id string) (Place, error)
	// List returns one page plus one more row when there is a next page.
	List(ctx context.Context, filter Filter) ([]Place, error)
	// Search finds active places of active cities whose name in any language
	// contains the query (or is close to it), best first.
	Search(ctx context.Context, query string, near *Coordinates, limit int) ([]Match, error)
}

type IDGenerator interface {
	NewID() string
}

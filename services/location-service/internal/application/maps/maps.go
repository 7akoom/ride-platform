// Package maps is what the apps need from a map: the route between two points (the
// line to draw, the distance, the time), a search for a place by name, and the name
// of a place from a point. It is a thin, validated layer over self-hosted OSRM
// (routes) and Nominatim (places), both built from OpenStreetMap.
package maps

import (
	"context"
	"errors"
)

var (
	ErrInvalidLatitude  = errors.New("latitude must be between -90 and 90")
	ErrInvalidLongitude = errors.New("longitude must be between -180 and 180")
	ErrPointRequired    = errors.New("a point is required")
	ErrQueryTooShort    = errors.New("query must have at least 2 characters")
	ErrQueryTooLong     = errors.New("query must have at most 200 characters")
	ErrInvalidLimit     = errors.New("limit must not be negative")
	ErrInvalidLanguage  = errors.New("language must be ar, ku or en")
	ErrTooManyVia       = errors.New("a route passes through at most 5 points")

	// ErrNoRoute means the road network has no way between the two points.
	ErrNoRoute = errors.New("no route between these points")

	// ErrNotNearRoad means a point is too far from any road to start or end a route.
	ErrNotNearRoad = errors.New("a point is not near a road")

	// ErrPlaceNotFound means nothing is known at that point.
	ErrPlaceNotFound = errors.New("no place found")

	// ErrUnavailable means the map server behind could not be reached or answered
	// with a failure: the caller may try again later.
	ErrUnavailable = errors.New("the map service is not available")
)

// Coordinates is a latitude and a longitude in degrees.
type Coordinates struct {
	Latitude  float64
	Longitude float64
}

// Validate reports whether the point is a real position on Earth.
func (c Coordinates) Validate() error {
	if c.Latitude < -90 || c.Latitude > 90 {
		return ErrInvalidLatitude
	}

	if c.Longitude < -180 || c.Longitude > 180 {
		return ErrInvalidLongitude
	}

	return nil
}

// Route is the best way by road between two points.
type Route struct {
	DistanceMeters  float64
	DurationSeconds float64

	// Polyline is the whole path as a Google encoded polyline with 5 digits of
	// precision (about a metre), ready to draw on a map.
	Polyline string
}

// Place is somewhere on the map: a named place, a street, an address.
type Place struct {
	// ID is the OpenStreetMap object, for example "way/123456".
	ID string

	// Name is the short name (a mall, a street); DisplayName is the full one with
	// the neighbourhood, city and country.
	Name        string
	DisplayName string

	Category    string
	Type        string
	Coordinates Coordinates

	// Address holds the parts of the address that are known (road, neighbourhood,
	// city, state, postcode...), by OpenStreetMap's own names.
	Address map[string]string
	// CuratedPlaceID is set for a curated place.
	CuratedPlaceID string
}

type RouteInput struct {
	Origin      *Coordinates
	Destination *Coordinates
	// Via are points to pass through on the way, in order (at most MaxVia).
	Via []*Coordinates
}

// MaxVia is how many points a route may pass through between its ends.
const MaxVia = 5

type SearchInput struct {
	Query string

	// Near, when set, ranks places close to it first (it does not exclude the rest).
	Near *Coordinates

	// Limit is how many places to return: 5 by default, at most 10.
	Limit int

	// Language is the language names are given in, when there is a choice: ar, ku
	// or en. Empty means Arabic, then Kurdish, then English.
	Language string
}

type ReverseInput struct {
	Coordinates *Coordinates
	Language    string
}

// CuratedSearcher finds curated places (see the place package) matching a
// search. languages is the ordered preference for names ("ar", "ku", "en").
type CuratedSearcher interface {
	SearchCurated(ctx context.Context, query string, near *Coordinates, limit int, languages []string) ([]Place, error)
}

// Router is the port to the routing engine (OSRM).
type Router interface {
	// Route goes from one point to another through via, in order.
	Route(ctx context.Context, from Coordinates, to Coordinates, via ...Coordinates) (Route, error)
}

// Geocoder is the port to the place search (Nominatim). languages is an ordered,
// comma-separated preference in the Accept-Language style ("ar,ku,en").
type Geocoder interface {
	Search(ctx context.Context, query string, near *Coordinates, limit int, languages string) ([]Place, error)
	Reverse(ctx context.Context, at Coordinates, languages string) (Place, error)
}

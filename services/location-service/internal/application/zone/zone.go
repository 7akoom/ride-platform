package zone

import (
	"regexp"
	"time"
)

var uuidShape = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// minBoundaryPoints is the fewest vertices that can form a real polygon.
// The stored ring is not closed (the first point is not repeated at the
// end) — closing happens at the persistence boundary, matching how the
// API represents it.
const minBoundaryPoints = 3

// Coordinates is a validated lat/lng pair. Defined locally rather than
// imported from the location package — every domain package in this
// codebase keeps its own copy of this value object rather than sharing
// one across domains with different lifecycles (see rider/trip/pricing
// for the same pattern).
type Coordinates struct {
	Latitude  float64
	Longitude float64
}

func NewCoordinates(latitude, longitude float64) (Coordinates, error) {
	if latitude < -90 || latitude > 90 {
		return Coordinates{}, ErrInvalidLatitude
	}

	if longitude < -180 || longitude > 180 {
		return Coordinates{}, ErrInvalidLongitude
	}

	return Coordinates{
		Latitude:  latitude,
		Longitude: longitude,
	}, nil
}

// NewBoundary validates a proposed polygon: every vertex must be a
// valid coordinate, and there must be enough of them to form a real
// shape (a 2-point "polygon" is just a line).
func NewBoundary(points []Coordinates) ([]Coordinates, error) {
	if len(points) < minBoundaryPoints {
		return nil, ErrBoundaryTooFewPoints
	}

	for _, point := range points {
		if point.Latitude < -90 || point.Latitude > 90 {
			return nil, ErrInvalidLatitude
		}

		if point.Longitude < -180 || point.Longitude > 180 {
			return nil, ErrInvalidLongitude
		}
	}

	boundary := make([]Coordinates, len(points))
	copy(boundary, points)

	return boundary, nil
}

// Zone is a served geographic area within a city — an arbitrary
// polygon, not the city itself. A city has as many zones as it needs.
type Zone struct {
	ID     string
	CityID string
	// City and TimeZone are the city's name and time zone, read with the zone.
	City      string
	TimeZone  string
	Name      string
	Boundary  []Coordinates
	Active    bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

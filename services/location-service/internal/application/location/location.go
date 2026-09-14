package location

import "time"

type EntityType string

const (
	EntityDriver EntityType = "driver"
	EntityRider  EntityType = "rider"
)

func (e EntityType) Valid() bool {
	switch e {
	case EntityDriver, EntityRider:
		return true
	default:
		return false
	}
}

// Coordinates is a validated lat/lng pair.
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

// Location is a point-in-time position for an entity (driver or rider).
// It is deliberately NOT a durable aggregate: it lives only in Valkey with
// a short TTL, because a position from more than a few seconds ago is not
// useful for dispatch. Trip service is responsible for persisting the
// handful of positions that matter for history (pickup, dropoff).
type Location struct {
	EntityType  EntityType
	EntityID    string
	Coordinates Coordinates
	UpdatedAt   time.Time
}

// NearbyEntity is one result of a proximity search.
type NearbyEntity struct {
	EntityID       string
	Coordinates    Coordinates
	DistanceMeters float64
}

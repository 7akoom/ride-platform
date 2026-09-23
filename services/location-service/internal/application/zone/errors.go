package zone

import "errors"

var (
	ErrCityRequired         = errors.New("city id is required")
	ErrCityNotFound         = errors.New("city not found")
	ErrNameRequired         = errors.New("name is required")
	ErrZoneIDRequired       = errors.New("zone id is required")
	ErrBoundaryTooFewPoints = errors.New("a zone boundary needs at least 3 points")
	ErrInvalidLatitude      = errors.New("latitude must be between -90 and 90")
	ErrInvalidLongitude     = errors.New("longitude must be between -180 and 180")

	ErrZoneNotFound = errors.New("zone not found")
)

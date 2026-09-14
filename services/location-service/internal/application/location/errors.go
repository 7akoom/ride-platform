package location

import "errors"

var (
	ErrEntityIDRequired  = errors.New("entity id is required")
	ErrInvalidEntityType = errors.New("invalid entity type")
	ErrInvalidLatitude   = errors.New("latitude must be between -90 and 90")
	ErrInvalidLongitude  = errors.New("longitude must be between -180 and 180")
	ErrInvalidRadius     = errors.New("radius must be greater than zero")

	ErrLocationNotFound = errors.New("location not found or stale")
)

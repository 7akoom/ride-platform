package trip

import "errors"

var (
	ErrRiderIDRequired  = errors.New("rider id is required")
	ErrDriverIDRequired = errors.New("driver id is required")
	ErrTripIDRequired   = errors.New("trip id is required")
	ErrInvalidLatitude  = errors.New("latitude must be between -90 and 90")
	ErrInvalidLongitude = errors.New("longitude must be between -180 and 180")

	ErrTripNotFound        = errors.New("trip not found")
	ErrInvalidTransition   = errors.New("trip cannot transition to the requested status from its current status")
	ErrRiderHasActiveTrip  = errors.New("rider already has an active trip")
	ErrDriverHasActiveTrip = errors.New("driver already has an active trip")
)

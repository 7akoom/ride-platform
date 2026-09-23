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

	ErrPickupOutsideServiceZone = errors.New("pickup location is outside every service zone")

	ErrInvalidSosTriggeredBy = errors.New("triggered_by must be rider or driver")

	ErrAddressTooLong = errors.New("an address is longer than 300 characters")

	// ErrSavedAddressesUnavailable: a saved address was named but this
	// service cannot read saved addresses.
	ErrSavedAddressesUnavailable = errors.New("saved addresses are not available")

	// ErrSavedAddressNotFound: the saved address is not one of the rider's.
	ErrSavedAddressNotFound = errors.New("saved address not found")

	// ErrUpstreamUnavailable: another service this needs (rider-service,
	// media-service) did not answer.
	ErrUpstreamUnavailable = errors.New("a service this needs is not available")
)

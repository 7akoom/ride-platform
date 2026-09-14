package dispatch

import "errors"

var (
	ErrTripIDRequired      = errors.New("trip id is required")
	ErrTripNotDispatchable = errors.New("trip is not in a dispatchable (requested) state")
	ErrNoDriversNearby     = errors.New("no drivers found near the pickup point")
	ErrNoDriversAvailable  = errors.New("nearby drivers were found but none could be assigned")
)

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

	// ErrRiderOwesFees: the rider owes cancelled trips' fees and the
	// deployment blocks new trips until they are paid.
	ErrRiderOwesFees = errors.New("the rider owes fees from cancelled trips; pay them to request a trip")

	ErrPickupOutsideServiceZone = errors.New("pickup location is outside every service zone")

	ErrInvalidSosTriggeredBy = errors.New("triggered_by must be rider or driver")

	ErrAddressTooLong = errors.New("an address is longer than 300 characters")

	// ErrInvalidPassenger: a trip for someone else needs their name (1-80
	// characters) and phone (E.164), both.
	ErrInvalidPassenger = errors.New("passenger_name (1-80 characters) and passenger_phone (E.164, like +9647701234567) go together")

	// ErrSavedAddressesUnavailable: a saved address was named but this
	// service cannot read saved addresses.
	ErrSavedAddressesUnavailable = errors.New("saved addresses are not available")

	// ErrSavedAddressNotFound: the saved address is not one of the rider's.
	ErrSavedAddressNotFound = errors.New("saved address not found")

	// ErrUpstreamUnavailable: another service this needs (rider-service,
	// media-service) did not answer.
	ErrUpstreamUnavailable = errors.New("a service this needs is not available")

	// ErrQuotesUnavailable: a quote was named but this service cannot claim
	// quotes.
	ErrQuotesUnavailable = errors.New("fare quotes are not available")

	// ErrQuoteNotFound: the quote is unknown or not the rider's.
	ErrQuoteNotFound = errors.New("quote not found")

	// ErrQuoteNotUsable: the quote expired, was used for another trip, or its
	// coupon can no longer be used. The rider asks for a new one.
	ErrQuoteNotUsable = errors.New("the quote can no longer be used; ask for a new one")

	// ErrQuoteMismatch: the trip's points or class are not the quoted ones.
	ErrQuoteMismatch = errors.New("the trip does not match its quote")

	ErrInvalidCancelledBy = errors.New("cancelled by must be rider, driver or system")

	// ErrNoShowOnlyByDriver: only the trip's driver cancels for a no-show.
	ErrNoShowOnlyByDriver = errors.New("only the driver can cancel because the rider did not show up")

	// ErrNoShowTooEarly: the driver has not marked arrival, or has not waited
	// at the pickup long enough since.
	ErrNoShowTooEarly = errors.New("a no-show can be reported only after arriving and waiting at the pickup")

	// ErrArrivalPositionUnknown: the driver has not reported a position in
	// the last few seconds, so their arrival cannot be checked.
	ErrArrivalPositionUnknown = errors.New("your position is not known; share your location and try again")

	// ErrTooFarFromPickup: the driver's last position is not at the pickup.
	ErrTooFarFromPickup = errors.New("you are not at the pickup yet")

	// ErrTooManyStops: a trip stops at most MaxStops times on the way.
	ErrTooManyStops = errors.New("a trip has at most 2 stops")

	// ErrStopNotFound: the trip has no stop at that position.
	ErrStopNotFound = errors.New("the trip has no such stop")

	// ErrTooFarFromStop: the driver's last position is not at the stop.
	ErrTooFarFromStop = errors.New("you are not at the stop yet")
)

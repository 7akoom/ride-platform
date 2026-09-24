package pricing

import "errors"

var (
	ErrRiderIDRequired  = errors.New("rider id is required")
	ErrTripIDRequired   = errors.New("trip id is required")
	ErrInvalidLatitude  = errors.New("latitude must be between -90 and 90")
	ErrInvalidLongitude = errors.New("longitude must be between -180 and 180")

	ErrNoActiveConfig = errors.New("no active pricing configuration found")

	// ErrPickupOutsideServiceZone mirrors trip-service's own check —
	ErrPickupOutsideServiceZone = errors.New("pickup location is outside every service zone")

	ErrCouponNotFound      = errors.New("coupon not found")
	ErrCouponAlreadyExists = errors.New("a coupon with this code already exists")
	// ErrCouponUnavailable: a coupon could not be held for a trip (it ended,
	// ran out, or the rider reached its limit in the meantime).
	ErrCouponUnavailable = errors.New("the coupon can no longer be used")

	// ErrFareAlreadyRecorded: the trip already has a fare (a concurrent
	// CalculateFare recorded it first).
	ErrFareAlreadyRecorded = errors.New("the trip already has a fare")

	ErrQuoteIDRequired  = errors.New("quote id is required")
	ErrQuoteNotFound    = errors.New("quote not found")
	ErrQuoteExpired     = errors.New("the quote has expired; ask for a new one")
	ErrQuoteAlreadyUsed = errors.New("the quote was already used for another trip; ask for a new one")
	// ErrQuoteCouponUnavailable: the coupon the quote used can no longer be
	// used (ended, used up, or the rider's limit reached).
	ErrQuoteCouponUnavailable = errors.New("the quote's coupon can no longer be used; ask for a new quote")
	// ErrQuoteNotForTrip: the trip names a quote another trip holds, or one
	// that is not its rider's.
	ErrQuoteNotForTrip = errors.New("the quote does not belong to this trip")
)

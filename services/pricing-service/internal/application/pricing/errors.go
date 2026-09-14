package pricing

import "errors"

var (
	ErrRiderIDRequired  = errors.New("rider id is required")
	ErrTripIDRequired   = errors.New("trip id is required")
	ErrInvalidLatitude  = errors.New("latitude must be between -90 and 90")
	ErrInvalidLongitude = errors.New("longitude must be between -180 and 180")

	ErrNoActiveConfig = errors.New("no active pricing configuration found")

	ErrCouponCodeRequired    = errors.New("coupon code is required")
	ErrCouponNotFound        = errors.New("coupon not found")
	ErrCouponAlreadyExists   = errors.New("a coupon with this code already exists")
	ErrInvalidDiscountType   = errors.New("invalid discount type")
	ErrInvalidDiscountValue  = errors.New("discount value must be positive (and at most 100 for percentage discounts)")
	ErrInvalidValidityWindow = errors.New("valid_until must be after valid_from")
)

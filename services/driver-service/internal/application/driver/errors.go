package driver

import "errors"

var (
	ErrDisplayNameRequired   = errors.New("driver display name is required")
	ErrDisplayNameTooLong    = errors.New("driver display name exceeds maximum length")
	ErrIdentityIDRequired    = errors.New("identity id is required")
	ErrDriverIDRequired      = errors.New("driver id is required")
	ErrVehicleFieldsRequired = errors.New("vehicle make, model, and plate number are required")
	ErrInvalidAvailability   = errors.New("invalid availability status")

	ErrDriverNotFound      = errors.New("driver not found")
	ErrDriverAlreadyExists = errors.New("driver already exists for this identity")
	ErrPlateNumberTaken    = errors.New("vehicle plate number is already registered")
)

package driver

import "errors"

var (
	ErrDisplayNameRequired   = errors.New("driver display name is required")
	ErrDisplayNameTooLong    = errors.New("driver display name exceeds maximum length")
	ErrIdentityIDRequired    = errors.New("identity id is required")
	ErrDriverIDRequired      = errors.New("driver id is required")
	ErrVehicleFieldsRequired = errors.New("vehicle make, model, and plate number are required")
	ErrInvalidVehicleClass   = errors.New("invalid vehicle class")
	ErrInvalidAvailability   = errors.New("invalid availability status")

	ErrDriverNotFound      = errors.New("driver not found")
	ErrDriverAlreadyExists = errors.New("driver already exists for this identity")
	ErrPlateNumberTaken    = errors.New("vehicle plate number is already registered")

	// ErrDriverNotApproved: the driver is pending or rejected and tried to go online.
	ErrDriverNotApproved = errors.New("driver is not approved")
	// ErrInvalidStatusTransition: the driver is in a status the requested change cannot start from.
	ErrInvalidStatusTransition = errors.New("driver status cannot be changed this way")
)

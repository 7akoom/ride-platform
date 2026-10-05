package driver

import "errors"

var (
	ErrDisplayNameRequired    = errors.New("driver display name is required")
	ErrDisplayNameTooLong     = errors.New("driver display name exceeds maximum length")
	ErrIdentityIDRequired     = errors.New("identity id is required")
	ErrDriverIDRequired       = errors.New("driver id is required")
	ErrVehicleFieldsRequired  = errors.New("vehicle make, model, and plate number are required")
	ErrVehicleFieldsTooLong   = errors.New("vehicle make and model are at most 60 characters, color 40, plate number 20")
	ErrInvalidVehicleClass    = errors.New("invalid vehicle class")
	ErrInvalidAvailability    = errors.New("invalid availability status")
	ErrInvalidVehicleYear     = errors.New("vehicle year must be between 1980 and next year")
	ErrRejectionReasonTooLong = errors.New("rejection reason exceeds maximum length")
	ErrInvalidListQuery       = errors.New("invalid driver list query")
	ErrInvalidPageToken       = errors.New("page_token is not valid")

	ErrDriverNotFound      = errors.New("driver not found")
	ErrDriverAlreadyExists = errors.New("driver already exists for this identity")
	ErrPlateNumberTaken    = errors.New("vehicle plate number is already registered")

	// ErrDriverNotApproved: the driver is pending or rejected and tried to go online.
	ErrDriverNotApproved = errors.New("driver is not approved")
	// ErrDocumentsIncomplete: a required document is missing, not yet approved, or out of date.
	ErrDocumentsIncomplete = errors.New("required driver documents are missing, not approved or out of date")
	// ErrProfileLocked: an approved driver's name and vehicle are only changed by staff.
	ErrProfileLocked = errors.New("the name and vehicle of an approved driver cannot be changed")
	// ErrInvalidStatusTransition: the driver is in a status the requested change cannot start from.
	ErrInvalidStatusTransition = errors.New("driver status cannot be changed this way")
)

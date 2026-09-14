package rider

import "errors"

var (
	ErrDisplayNameRequired = errors.New("rider display name is required")
	ErrDisplayNameTooLong  = errors.New("rider display name exceeds maximum length")
	ErrIdentityIDRequired  = errors.New("identity id is required")
	ErrRiderIDRequired     = errors.New("rider id is required")

	ErrRiderNotFound      = errors.New("rider not found")
	ErrRiderAlreadyExists = errors.New("rider already exists for this identity")
	ErrRiderSuspended     = errors.New("rider is suspended")
)

package auth

import "errors"

type IdentityStatus string

const (
	IdentityStatusActive    IdentityStatus = "active"
	IdentityStatusSuspended IdentityStatus = "suspended"
	IdentityStatusDisabled  IdentityStatus = "disabled"
)

var ErrInvalidIdentityStatus = errors.New(
	"invalid identity status",
)

func ParseIdentityStatus(
	value string,
) (IdentityStatus, error) {
	status := IdentityStatus(value)

	switch status {
	case IdentityStatusActive,
		IdentityStatusSuspended,
		IdentityStatusDisabled:
		return status, nil

	default:
		return "", ErrInvalidIdentityStatus
	}
}

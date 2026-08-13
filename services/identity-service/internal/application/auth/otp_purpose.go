package auth

import (
	"errors"
	"strings"
)

type OTPPurpose string

const (
	OTPPurposeLogin          OTPPurpose = "login"
	OTPPurposeLinkIdentifier OTPPurpose = "link_identifier"
)

var ErrInvalidOTPPurpose = errors.New(
	"invalid OTP purpose",
)

func ParseOTPPurpose(
	value string,
) (OTPPurpose, error) {
	normalized := strings.ToLower(
		strings.TrimSpace(value),
	)

	switch OTPPurpose(normalized) {
	case OTPPurposeLogin:
		return OTPPurposeLogin, nil

	case OTPPurposeLinkIdentifier:
		return OTPPurposeLinkIdentifier, nil

	default:
		return "", ErrInvalidOTPPurpose
	}
}

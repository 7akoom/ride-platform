package auth

import (
	"errors"
	"strings"
)

type OTPPurpose string

const (
	OTPPurposeLogin            OTPPurpose = "login"
	OTPPurposeLinkIdentifier   OTPPurpose = "link_identifier"
	OTPPurposeUnlinkIdentifier OTPPurpose = "unlink_identifier"
	// OTPPurposeDeleteAccount confirms deleting the account (target identity set).
	OTPPurposeDeleteAccount OTPPurpose = "delete_account"
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

	case OTPPurposeUnlinkIdentifier:
		return OTPPurposeUnlinkIdentifier, nil

	case OTPPurposeDeleteAccount:
		return OTPPurposeDeleteAccount, nil

	default:
		return "", ErrInvalidOTPPurpose
	}
}

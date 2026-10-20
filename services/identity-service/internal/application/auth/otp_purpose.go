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

// OTPOperationConfirmIdentity is a login code asked for by a signed-in person to
// prove it is them again (resetting the wallet PIN): it has its own cooldown, apart
// from signing in, and counts towards the same per-identifier window.
const OTPOperationConfirmIdentity = "login:confirm_identity"

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

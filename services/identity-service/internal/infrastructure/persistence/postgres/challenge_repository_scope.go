package postgres

import (
	"errors"
	"fmt"
	"strings"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

func normalizeChallengeTargetIdentityID(
	purpose auth.OTPPurpose,
	targetIdentityID *string,
) (any, error) {
	switch purpose {
	case auth.OTPPurposeLogin:
		if targetIdentityID != nil {
			return nil, errors.New(
				"login OTP challenge cannot target an identity",
			)
		}

		return nil, nil

	case auth.OTPPurposeLinkIdentifier:
		if targetIdentityID == nil {
			return nil, errors.New(
				"link identifier OTP challenge requires target identity",
			)
		}

		normalized := strings.TrimSpace(
			*targetIdentityID,
		)
		if normalized == "" {
			return nil, errors.New(
				"OTP challenge target identity cannot be blank",
			)
		}

		return normalized, nil

	default:
		return nil, auth.ErrInvalidOTPPurpose
	}
}

func challengeScopeLockKey(
	identifier auth.Identifier,
	purpose auth.OTPPurpose,
	targetIdentityID any,
	tenantHint any,
) string {
	target := "-"

	if targetIdentityID != nil {
		target = fmt.Sprint(targetIdentityID)
	}

	tenant := "-"

	if tenantHint != nil {
		tenant = fmt.Sprint(tenantHint)
	}

	return tenant +
		":" +
		string(identifier.Type) +
		":" +
		identifier.Value +
		":" +
		string(purpose) +
		":" +
		target
}
func normalizeChallengeTenantHint(
	value string,
) (any, error) {
	tenantHint, err := auth.NormalizeTenantHint(value)
	if err != nil {
		return nil, err
	}

	if tenantHint == "" {
		return nil, nil
	}

	return tenantHint, nil
}

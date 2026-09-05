package auth

import (
	"errors"
	"strings"
)

const maxTenantHintLength = 128

func NormalizeTenantHint(value string) (string, error) {
	value = strings.TrimSpace(value)

	if value == "" {
		return "", nil
	}

	if len(value) > maxTenantHintLength {
		return "", errors.New(
			"tenant hint cannot exceed 128 characters",
		)
	}

	return value, nil
}

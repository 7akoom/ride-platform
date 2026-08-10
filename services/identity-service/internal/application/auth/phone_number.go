package auth

import (
	"errors"
	"regexp"
	"strings"
)

var ErrInvalidPhoneNumber = errors.New(
	"invalid phone number",
)

var e164PhoneNumberPattern = regexp.MustCompile(
	`^\+[1-9][0-9]{1,14}$`,
)

func NormalizePhoneNumber(
	phoneNumber string,
) (string, error) {
	normalized := strings.TrimSpace(phoneNumber)

	if !e164PhoneNumberPattern.MatchString(normalized) {
		return "", ErrInvalidPhoneNumber
	}

	return normalized, nil
}
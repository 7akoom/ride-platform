package auth

import (
	"errors"
	"net/mail"
	"strings"
)

var ErrInvalidEmailAddress = errors.New(
	"invalid email address",
)

func NormalizeEmailAddress(
	emailAddress string,
) (string, error) {
	normalized := strings.TrimSpace(emailAddress)

	if normalized == "" || len(normalized) > 254 {
		return "", ErrInvalidEmailAddress
	}

	if strings.ContainsAny(normalized, "\r\n\t ") {
		return "", ErrInvalidEmailAddress
	}

	parsed, err := mail.ParseAddress(normalized)
	if err != nil || parsed.Address != normalized {
		return "", ErrInvalidEmailAddress
	}

	at := strings.LastIndexByte(normalized, '@')
	if at <= 0 || at == len(normalized)-1 {
		return "", ErrInvalidEmailAddress
	}

	localPart := normalized[:at]
	domain := normalized[at+1:]

	if len(localPart) > 64 ||
		domain == "" ||
		strings.HasPrefix(domain, ".") ||
		strings.HasSuffix(domain, ".") ||
		strings.Contains(domain, "..") {
		return "", ErrInvalidEmailAddress
	}

	return strings.ToLower(
		localPart + "@" + domain,
	), nil
}

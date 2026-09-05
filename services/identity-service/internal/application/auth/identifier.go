package auth

import (
	"errors"
	"strings"
)

type IdentifierType string

const (
	IdentifierTypePhone IdentifierType = "phone"
	IdentifierTypeEmail IdentifierType = "email"
)

var ErrInvalidIdentifierType = errors.New(
	"invalid identity identifier type",
)

type Identifier struct {
	Type  IdentifierType
	Value string
}

func NewIdentifier(
	identifierType IdentifierType,
	value string,
) (Identifier, error) {
	switch identifierType {
	case IdentifierTypePhone:
		normalized, err := NormalizePhoneNumber(value)
		if err != nil {
			return Identifier{}, err
		}

		return Identifier{
			Type:  IdentifierTypePhone,
			Value: normalized,
		}, nil

	case IdentifierTypeEmail:
		normalized, err := NormalizeEmailAddress(value)
		if err != nil {
			return Identifier{}, err
		}

		return Identifier{
			Type:  IdentifierTypeEmail,
			Value: normalized,
		}, nil

	default:
		return Identifier{}, ErrInvalidIdentifierType
	}
}

func ParseIdentifierType(
	value string,
) (IdentifierType, error) {
	normalized := strings.ToLower(
		strings.TrimSpace(value),
	)

	switch IdentifierType(normalized) {
	case IdentifierTypePhone:
		return IdentifierTypePhone, nil

	case IdentifierTypeEmail:
		return IdentifierTypeEmail, nil

	default:
		return "", ErrInvalidIdentifierType
	}
}

package auth

import (
	"errors"
	"testing"
)

func TestNewIdentifierCreatesPhoneIdentifier(t *testing.T) {
	identifier, err := NewIdentifier(
		IdentifierTypePhone,
		"  +9647500000000  ",
	)
	if err != nil {
		t.Fatalf("NewIdentifier() returned unexpected error: %v", err)
	}

	if identifier.Type != IdentifierTypePhone {
		t.Fatalf(
			"identifier.Type = %q, want %q",
			identifier.Type,
			IdentifierTypePhone,
		)
	}

	if identifier.Value != "+9647500000000" {
		t.Fatalf(
			"identifier.Value = %q, want %q",
			identifier.Value,
			"+9647500000000",
		)
	}
}

func TestNewIdentifierCreatesEmailIdentifier(t *testing.T) {
	identifier, err := NewIdentifier(
		IdentifierTypeEmail,
		"  User@EXAMPLE.COM  ",
	)
	if err != nil {
		t.Fatalf("NewIdentifier() returned unexpected error: %v", err)
	}

	if identifier.Type != IdentifierTypeEmail {
		t.Fatalf(
			"identifier.Type = %q, want %q",
			identifier.Type,
			IdentifierTypeEmail,
		)
	}

	if identifier.Value != "user@example.com" {
		t.Fatalf(
			"identifier.Value = %q, want %q",
			identifier.Value,
			"user@example.com",
		)
	}
}

func TestNewIdentifierRejectsInvalidPhoneNumber(t *testing.T) {
	_, err := NewIdentifier(
		IdentifierTypePhone,
		"07500000000",
	)

	if !errors.Is(err, ErrInvalidPhoneNumber) {
		t.Fatalf(
			"NewIdentifier() error = %v, want %v",
			err,
			ErrInvalidPhoneNumber,
		)
	}
}

func TestNewIdentifierRejectsInvalidEmailAddress(t *testing.T) {
	_, err := NewIdentifier(
		IdentifierTypeEmail,
		"invalid-email",
	)

	if !errors.Is(err, ErrInvalidEmailAddress) {
		t.Fatalf(
			"NewIdentifier() error = %v, want %v",
			err,
			ErrInvalidEmailAddress,
		)
	}
}

func TestNewIdentifierRejectsUnsupportedIdentifierType(t *testing.T) {
	_, err := NewIdentifier(
		IdentifierType("username"),
		"user",
	)

	if !errors.Is(err, ErrInvalidIdentifierType) {
		t.Fatalf(
			"NewIdentifier() error = %v, want %v",
			err,
			ErrInvalidIdentifierType,
		)
	}
}

func TestParseIdentifierTypeAcceptsSupportedTypes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected IdentifierType
	}{
		{
			name:     "phone",
			input:    "phone",
			expected: IdentifierTypePhone,
		},
		{
			name:     "email",
			input:    "email",
			expected: IdentifierTypeEmail,
		},
		{
			name:     "trims surrounding spaces",
			input:    "  phone  ",
			expected: IdentifierTypePhone,
		},
		{
			name:     "normalizes casing",
			input:    "EMAIL",
			expected: IdentifierTypeEmail,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := ParseIdentifierType(tt.input)
			if err != nil {
				t.Fatalf(
					"ParseIdentifierType(%q) returned unexpected error: %v",
					tt.input,
					err,
				)
			}

			if actual != tt.expected {
				t.Fatalf(
					"ParseIdentifierType(%q) = %q, want %q",
					tt.input,
					actual,
					tt.expected,
				)
			}
		})
	}
}

func TestParseIdentifierTypeRejectsUnsupportedType(t *testing.T) {
	_, err := ParseIdentifierType("username")

	if !errors.Is(err, ErrInvalidIdentifierType) {
		t.Fatalf(
			"ParseIdentifierType() error = %v, want %v",
			err,
			ErrInvalidIdentifierType,
		)
	}
}

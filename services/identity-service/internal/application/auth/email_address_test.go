package auth

import (
	"errors"
	"testing"
)

func TestNormalizeEmailAddressAcceptsValidAddresses(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple address",
			input:    "user@example.com",
			expected: "user@example.com",
		},
		{
			name:     "trims surrounding spaces",
			input:    "  user@example.com  ",
			expected: "user@example.com",
		},
		{
			name:     "normalizes domain to lowercase",
			input:    "user@EXAMPLE.COM",
			expected: "user@example.com",
		},
		{
			name:     "normalizes entire address to lowercase",
			input:    "User@EXAMPLE.COM",
			expected: "user@example.com",
		},
		{
			name:     "supports plus addressing",
			input:    "user+rides@example.com",
			expected: "user+rides@example.com",
		},
		{
			name:     "supports subdomains",
			input:    "user@mail.example.com",
			expected: "user@mail.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := NormalizeEmailAddress(tt.input)
			if err != nil {
				t.Fatalf(
					"NormalizeEmailAddress(%q) returned unexpected error: %v",
					tt.input,
					err,
				)
			}

			if actual != tt.expected {
				t.Fatalf(
					"NormalizeEmailAddress(%q) = %q, want %q",
					tt.input,
					actual,
					tt.expected,
				)
			}
		})
	}
}

func TestNormalizeEmailAddressProducesSameCanonicalValueRegardlessOfCase(
	t *testing.T,
) {
	first, err := NormalizeEmailAddress("User@Example.COM")
	if err != nil {
		t.Fatalf(
			"NormalizeEmailAddress() returned unexpected error: %v",
			err,
		)
	}

	second, err := NormalizeEmailAddress("user@example.com")
	if err != nil {
		t.Fatalf(
			"NormalizeEmailAddress() returned unexpected error: %v",
			err,
		)
	}

	if first != second {
		t.Fatalf(
			"canonical email mismatch: %q != %q",
			first,
			second,
		)
	}
}

func TestNormalizeEmailAddressRejectsInvalidAddresses(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "empty",
			input: "",
		},
		{
			name:  "spaces only",
			input: "   ",
		},
		{
			name:  "missing at sign",
			input: "user.example.com",
		},
		{
			name:  "missing local part",
			input: "@example.com",
		},
		{
			name:  "missing domain",
			input: "user@",
		},
		{
			name:  "contains internal space",
			input: "user name@example.com",
		},
		{
			name:  "contains tab",
			input: "user\t@example.com",
		},
		{
			name:  "contains newline",
			input: "user\n@example.com",
		},
		{
			name:  "domain starts with dot",
			input: "user@.example.com",
		},
		{
			name:  "domain ends with dot",
			input: "user@example.com.",
		},
		{
			name:  "domain contains consecutive dots",
			input: "user@example..com",
		},
		{
			name:  "display name is not accepted",
			input: "User <user@example.com>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NormalizeEmailAddress(tt.input)
			if !errors.Is(err, ErrInvalidEmailAddress) {
				t.Fatalf(
					"NormalizeEmailAddress(%q) error = %v, want %v",
					tt.input,
					err,
					ErrInvalidEmailAddress,
				)
			}
		})
	}
}

func TestNormalizeEmailAddressRejectsLocalPartLongerThan64Characters(t *testing.T) {
	localPart := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	_, err := NormalizeEmailAddress(localPart + "@example.com")
	if !errors.Is(err, ErrInvalidEmailAddress) {
		t.Fatalf(
			"NormalizeEmailAddress() error = %v, want %v",
			err,
			ErrInvalidEmailAddress,
		)
	}
}

func TestNormalizeEmailAddressRejectsAddressLongerThan254Characters(t *testing.T) {
	localPart := "user"
	domain := ""

	for len(localPart)+1+len(domain) <= 254 {
		if domain != "" {
			domain += "."
		}

		domain += "example"
	}

	_, err := NormalizeEmailAddress(localPart + "@" + domain)
	if !errors.Is(err, ErrInvalidEmailAddress) {
		t.Fatalf(
			"NormalizeEmailAddress() error = %v, want %v",
			err,
			ErrInvalidEmailAddress,
		)
	}
}

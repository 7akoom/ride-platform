package auth

import (
	"errors"
	"testing"
)

func TestNormalizePhoneNumberAcceptsValidE164Numbers(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Iraq",
			input:    "+9647501234567",
			expected: "+9647501234567",
		},
		{
			name:     "Syria",
			input:    "+963944123456",
			expected: "+963944123456",
		},
		{
			name:     "Jordan",
			input:    "+962791234567",
			expected: "+962791234567",
		},
		{
			name:     "trims surrounding spaces",
			input:    "   +9647501234567   ",
			expected: "+9647501234567",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			normalized, err := NormalizePhoneNumber(
				test.input,
			)
			if err != nil {
				t.Fatalf(
					"NormalizePhoneNumber() returned error: %v",
					err,
				)
			}

			if normalized != test.expected {
				t.Fatalf(
					"normalized phone number is %q, expected %q",
					normalized,
					test.expected,
				)
			}
		})
	}
}

func TestNormalizePhoneNumberRejectsInvalidNumbers(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "empty",
			input: "",
		},
		{
			name:  "local format",
			input: "07501234567",
		},
		{
			name:  "international prefix without plus",
			input: "009647501234567",
		},
		{
			name:  "spaces inside number",
			input: "+964 750 123 4567",
		},
		{
			name:  "letters",
			input: "+964750ABC4567",
		},
		{
			name:  "country code starts with zero",
			input: "+0123456789",
		},
		{
			name:  "too long",
			input: "+1234567890123456",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NormalizePhoneNumber(
				test.input,
			)

			if !errors.Is(
				err,
				ErrInvalidPhoneNumber,
			) {
				t.Fatalf(
					"NormalizePhoneNumber() returned %v, expected %v",
					err,
					ErrInvalidPhoneNumber,
				)
			}
		})
	}
}
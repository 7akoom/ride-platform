package auth

import (
	"errors"
	"testing"
)

func TestParseOTPPurposeAcceptsSupportedPurposes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected OTPPurpose
	}{
		{
			name:     "login",
			input:    "login",
			expected: OTPPurposeLogin,
		},
		{
			name:     "link identifier",
			input:    "link_identifier",
			expected: OTPPurposeLinkIdentifier,
		},
		{
			name:     "trims surrounding spaces",
			input:    "  login  ",
			expected: OTPPurposeLogin,
		},
		{
			name:     "normalizes casing",
			input:    "LINK_IDENTIFIER",
			expected: OTPPurposeLinkIdentifier,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := ParseOTPPurpose(tt.input)
			if err != nil {
				t.Fatalf(
					"ParseOTPPurpose(%q) returned unexpected error: %v",
					tt.input,
					err,
				)
			}

			if actual != tt.expected {
				t.Fatalf(
					"ParseOTPPurpose(%q) = %q, want %q",
					tt.input,
					actual,
					tt.expected,
				)
			}
		})
	}
}

func TestParseOTPPurposeRejectsUnsupportedPurpose(t *testing.T) {
	_, err := ParseOTPPurpose("password_reset")

	if !errors.Is(err, ErrInvalidOTPPurpose) {
		t.Fatalf(
			"ParseOTPPurpose() error = %v, want %v",
			err,
			ErrInvalidOTPPurpose,
		)
	}
}

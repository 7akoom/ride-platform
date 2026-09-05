package auth

import (
	"errors"
	"testing"
)

func TestParseOTPDeliveryChannelAcceptsSupportedValues(
	t *testing.T,
) {
	tests := []struct {
		name     string
		value    string
		expected OTPDeliveryChannel
	}{
		{
			name:     "blank defaults to auto",
			value:    "",
			expected: OTPDeliveryChannelAuto,
		},
		{
			name:     "whitespace defaults to auto",
			value:    "   ",
			expected: OTPDeliveryChannelAuto,
		},
		{
			name:     "auto",
			value:    "auto",
			expected: OTPDeliveryChannelAuto,
		},
		{
			name:     "sms",
			value:    "sms",
			expected: OTPDeliveryChannelSMS,
		},
		{
			name:     "WhatsApp",
			value:    "whatsapp",
			expected: OTPDeliveryChannelWhatsApp,
		},
		{
			name:     "email",
			value:    "email",
			expected: OTPDeliveryChannelEmail,
		},
		{
			name:     "normalizes auto",
			value:    "  AUTO  ",
			expected: OTPDeliveryChannelAuto,
		},
		{
			name:     "normalizes SMS",
			value:    "  SMS  ",
			expected: OTPDeliveryChannelSMS,
		},
		{
			name:     "normalizes WhatsApp",
			value:    "  WhatsApp  ",
			expected: OTPDeliveryChannelWhatsApp,
		},
		{
			name:     "normalizes email",
			value:    "  EMAIL  ",
			expected: OTPDeliveryChannelEmail,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			channel, err := ParseOTPDeliveryChannel(
				testCase.value,
			)
			if err != nil {
				t.Fatalf(
					"ParseOTPDeliveryChannel(%q) returned an error: %v",
					testCase.value,
					err,
				)
			}

			if channel != testCase.expected {
				t.Fatalf(
					"ParseOTPDeliveryChannel(%q) = %q, expected %q",
					testCase.value,
					channel,
					testCase.expected,
				)
			}
		})
	}
}

func TestParseOTPDeliveryChannelRejectsUnsupportedValues(
	t *testing.T,
) {
	tests := []string{
		"telegram",
		"push",
		"mail",
		"text",
		"whats-app",
		"sms,email",
		"123",
	}

	for _, value := range tests {
		t.Run(value, func(t *testing.T) {
			channel, err := ParseOTPDeliveryChannel(
				value,
			)

			if !errors.Is(
				err,
				ErrInvalidOTPDeliveryChannel,
			) {
				t.Fatalf(
					"ParseOTPDeliveryChannel(%q) error = %v, expected %v",
					value,
					err,
					ErrInvalidOTPDeliveryChannel,
				)
			}

			if channel != "" {
				t.Fatalf(
					"ParseOTPDeliveryChannel(%q) = %q, expected empty channel",
					value,
					channel,
				)
			}
		})
	}
}

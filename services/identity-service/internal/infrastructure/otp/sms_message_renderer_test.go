package otp

import (
	"testing"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

func TestDefaultSMSMessageRendererRendersOTPPurposes(
	t *testing.T,
) {
	renderer, err := NewDefaultSMSMessageRenderer(
		"Ride",
	)
	if err != nil {
		t.Fatalf(
			"NewDefaultSMSMessageRenderer() returned an error: %v",
			err,
		)
	}

	tests := []struct {
		name     string
		purpose  auth.OTPPurpose
		locale   string
		expected string
	}{
		{
			name:    "login English",
			purpose: auth.OTPPurposeLogin,
			locale:  "en",
			expected: "Ride verification code: 123456. " +
				"Do not share this code.",
		},
		{
			name:    "login Arabic",
			purpose: auth.OTPPurposeLogin,
			locale:  "ar",
			expected: "رمز التحقق الخاص بـ Ride هو 123456. " +
				"لا تشارك هذا الرمز مع أي شخص.",
		},
		{
			name:    "login Kurdish",
			purpose: auth.OTPPurposeLogin,
			locale:  "ku",
			expected: "کۆدی پشتڕاستکردنەوەی Ride: 123456. " +
				"ئەم کۆدە لەگەڵ کەسێک هاوبەش مەکە.",
		},
		{
			name:    "link identifier English",
			purpose: auth.OTPPurposeLinkIdentifier,
			locale:  "en",
			expected: "Ride code to verify a new sign-in method: " +
				"123456. Do not share this code.",
		},
		{
			name:    "link identifier Arabic",
			purpose: auth.OTPPurposeLinkIdentifier,
			locale:  "ar",
			expected: "رمز Ride لتأكيد وسيلة تسجيل دخول جديدة هو 123456. " +
				"لا تشارك هذا الرمز مع أي شخص.",
		},
		{
			name:    "link identifier Kurdish",
			purpose: auth.OTPPurposeLinkIdentifier,
			locale:  "ku",
			expected: "کۆدی Ride بۆ پشتڕاستکردنەوەی شێوازی نوێی چوونەژوورەوە: " +
				"123456. ئەم کۆدە لەگەڵ کەسێک هاوبەش مەکە.",
		},
		{
			name:    "unlink identifier English",
			purpose: auth.OTPPurposeUnlinkIdentifier,
			locale:  "en",
			expected: "Ride code to remove a sign-in method: " +
				"123456. Do not share this code.",
		},
		{
			name:    "unlink identifier Arabic",
			purpose: auth.OTPPurposeUnlinkIdentifier,
			locale:  "ar",
			expected: "رمز Ride لإزالة وسيلة تسجيل دخول هو 123456. " +
				"لا تشارك هذا الرمز مع أي شخص.",
		},
		{
			name:    "unlink identifier Kurdish",
			purpose: auth.OTPPurposeUnlinkIdentifier,
			locale:  "ku",
			expected: "کۆدی Ride بۆ سڕینەوەی شێوازی چوونەژوورەوە: " +
				"123456. ئەم کۆدە لەگەڵ کەسێک هاوبەش مەکە.",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			message, err := renderer.Render(
				OTPMessageRenderInput{
					Code:    " 123456 ",
					Purpose: testCase.purpose,
					Locale:  testCase.locale,
				},
			)
			if err != nil {
				t.Fatalf(
					"Render() returned an error: %v",
					err,
				)
			}

			if message != testCase.expected {
				t.Fatalf(
					"message = %q, expected %q",
					message,
					testCase.expected,
				)
			}
		})
	}
}

func TestDefaultSMSMessageRendererNormalizesLocale(
	t *testing.T,
) {
	renderer, err := NewDefaultSMSMessageRenderer(
		"Ride",
	)
	if err != nil {
		t.Fatalf(
			"NewDefaultSMSMessageRenderer() returned an error: %v",
			err,
		)
	}

	tests := []struct {
		name     string
		locale   string
		expected string
	}{
		{
			name:   "Arabic regional locale",
			locale: "ar-IQ",
			expected: "رمز التحقق الخاص بـ Ride هو 123456. " +
				"لا تشارك هذا الرمز مع أي شخص.",
		},
		{
			name:   "Kurdish regional locale with underscore",
			locale: "ku_IQ",
			expected: "کۆدی پشتڕاستکردنەوەی Ride: 123456. " +
				"ئەم کۆدە لەگەڵ کەسێک هاوبەش مەکە.",
		},
		{
			name:   "English regional locale",
			locale: "en-US",
			expected: "Ride verification code: 123456. " +
				"Do not share this code.",
		},
		{
			name:   "blank locale defaults to English",
			locale: "",
			expected: "Ride verification code: 123456. " +
				"Do not share this code.",
		},
		{
			name:   "unsupported locale defaults to English",
			locale: "fr",
			expected: "Ride verification code: 123456. " +
				"Do not share this code.",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			message, err := renderer.Render(
				OTPMessageRenderInput{
					Code:    "123456",
					Purpose: auth.OTPPurposeLogin,
					Locale:  testCase.locale,
				},
			)
			if err != nil {
				t.Fatalf(
					"Render() returned an error: %v",
					err,
				)
			}

			if message != testCase.expected {
				t.Fatalf(
					"message = %q, expected %q",
					message,
					testCase.expected,
				)
			}
		})
	}
}

func TestDefaultSMSMessageRendererRejectsInvalidInput(
	t *testing.T,
) {
	renderer, err := NewDefaultSMSMessageRenderer(
		"Ride",
	)
	if err != nil {
		t.Fatalf(
			"NewDefaultSMSMessageRenderer() returned an error: %v",
			err,
		)
	}

	tests := []OTPMessageRenderInput{
		{
			Code:    " ",
			Purpose: auth.OTPPurposeLogin,
		},
		{
			Code:    "123456",
			Purpose: auth.OTPPurpose("unknown"),
		},
	}

	for _, input := range tests {
		message, err := renderer.Render(input)

		if err == nil {
			t.Fatal(
				"Render() accepted invalid input",
			)
		}

		if message != "" {
			t.Fatalf(
				"message = %q, expected empty message",
				message,
			)
		}
	}
}

func TestNewDefaultSMSMessageRendererRequiresBrandName(
	t *testing.T,
) {
	renderer, err := NewDefaultSMSMessageRenderer(
		"   ",
	)

	if err == nil {
		t.Fatal(
			"NewDefaultSMSMessageRenderer() accepted blank brand name",
		)
	}

	if renderer != nil {
		t.Fatal(
			"NewDefaultSMSMessageRenderer() returned renderer with blank brand name",
		)
	}
}

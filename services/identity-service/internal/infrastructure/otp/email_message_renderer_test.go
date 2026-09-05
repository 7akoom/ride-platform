package otp

import (
	"strings"
	"testing"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

func TestDefaultEmailMessageRendererRendersLocalizedLoginEmail(
	t *testing.T,
) {
	renderer, err := NewDefaultEmailMessageRenderer(
		"Ride",
	)
	if err != nil {
		t.Fatalf(
			"NewDefaultEmailMessageRenderer() returned an error: %v",
			err,
		)
	}

	tests := []struct {
		name             string
		locale           string
		expectedSubject  string
		expectedTextBody string
		expectedDir      string
	}{
		{
			name:            "English",
			locale:          "en",
			expectedSubject: "Ride verification code",
			expectedTextBody: "Ride verification code: 123456. " +
				"Do not share this code with anyone.",
			expectedDir: `dir="ltr"`,
		},
		{
			name:            "Arabic",
			locale:          "ar-IQ",
			expectedSubject: "رمز التحقق من Ride",
			expectedTextBody: "رمز التحقق الخاص بـ Ride هو 123456. " +
				"لا تشارك هذا الرمز مع أي شخص.",
			expectedDir: `dir="rtl"`,
		},
		{
			name:            "Kurdish",
			locale:          "ku-IQ",
			expectedSubject: "کۆدی پشتڕاستکردنەوەی Ride",
			expectedTextBody: "کۆدی پشتڕاستکردنەوەی Ride بریتییە لە 123456. " +
				"ئەم کۆدە لەگەڵ هیچ کەسێک هاوبەش مەکە.",
			expectedDir: `dir="rtl"`,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			message, err := renderer.Render(
				OTPEmailMessageRenderInput{
					Code:    " 123456 ",
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

			if message.Subject !=
				testCase.expectedSubject {
				t.Fatalf(
					"subject = %q, expected %q",
					message.Subject,
					testCase.expectedSubject,
				)
			}

			if message.TextBody !=
				testCase.expectedTextBody {
				t.Fatalf(
					"text body = %q, expected %q",
					message.TextBody,
					testCase.expectedTextBody,
				)
			}

			if !strings.Contains(
				message.HTMLBody,
				testCase.expectedDir,
			) {
				t.Fatalf(
					"HTML body = %q, expected %q",
					message.HTMLBody,
					testCase.expectedDir,
				)
			}

			if !strings.Contains(
				message.HTMLBody,
				"123456",
			) {
				t.Fatalf(
					"HTML body does not contain OTP code: %q",
					message.HTMLBody,
				)
			}
		})
	}
}

func TestDefaultEmailMessageRendererRendersAllOTPPurposes(
	t *testing.T,
) {
	renderer, err := NewDefaultEmailMessageRenderer(
		"Ride",
	)
	if err != nil {
		t.Fatalf(
			"NewDefaultEmailMessageRenderer() returned an error: %v",
			err,
		)
	}

	tests := []auth.OTPPurpose{
		auth.OTPPurposeLogin,
		auth.OTPPurposeLinkIdentifier,
		auth.OTPPurposeUnlinkIdentifier,
	}

	for _, purpose := range tests {
		message, err := renderer.Render(
			OTPEmailMessageRenderInput{
				Code:    "123456",
				Purpose: purpose,
				Locale:  "en",
			},
		)
		if err != nil {
			t.Fatalf(
				"Render(%q) returned an error: %v",
				purpose,
				err,
			)
		}

		if strings.TrimSpace(
			message.Subject,
		) == "" {
			t.Fatalf(
				"Render(%q) returned blank subject",
				purpose,
			)
		}

		if strings.TrimSpace(
			message.TextBody,
		) == "" {
			t.Fatalf(
				"Render(%q) returned blank text body",
				purpose,
			)
		}

		if strings.TrimSpace(
			message.HTMLBody,
		) == "" {
			t.Fatalf(
				"Render(%q) returned blank HTML body",
				purpose,
			)
		}
	}
}

func TestDefaultEmailMessageRendererDefaultsUnsupportedLocaleToEnglish(
	t *testing.T,
) {
	renderer, err := NewDefaultEmailMessageRenderer(
		"Ride",
	)
	if err != nil {
		t.Fatalf(
			"NewDefaultEmailMessageRenderer() returned an error: %v",
			err,
		)
	}

	message, err := renderer.Render(
		OTPEmailMessageRenderInput{
			Code:    "123456",
			Purpose: auth.OTPPurposeLogin,
			Locale:  "fr-FR",
		},
	)
	if err != nil {
		t.Fatalf(
			"Render() returned an error: %v",
			err,
		)
	}

	if message.Subject !=
		"Ride verification code" {
		t.Fatalf(
			"subject = %q, expected English fallback",
			message.Subject,
		)
	}
}

func TestDefaultEmailMessageRendererRejectsInvalidInput(
	t *testing.T,
) {
	renderer, err := NewDefaultEmailMessageRenderer(
		"Ride",
	)
	if err != nil {
		t.Fatalf(
			"NewDefaultEmailMessageRenderer() returned an error: %v",
			err,
		)
	}

	if _, err := renderer.Render(
		OTPEmailMessageRenderInput{
			Purpose: auth.OTPPurposeLogin,
			Locale:  "en",
		},
	); err == nil {
		t.Fatal(
			"Render() accepted blank OTP code",
		)
	}

	if _, err := renderer.Render(
		OTPEmailMessageRenderInput{
			Code:    "123456",
			Purpose: auth.OTPPurpose("invalid"),
			Locale:  "en",
		},
	); err == nil {
		t.Fatal(
			"Render() accepted invalid OTP purpose",
		)
	}
}

func TestNewDefaultEmailMessageRendererRequiresBrandName(
	t *testing.T,
) {
	if _, err := NewDefaultEmailMessageRenderer(
		"   ",
	); err == nil {
		t.Fatal(
			"NewDefaultEmailMessageRenderer() accepted blank brand name",
		)
	}
}

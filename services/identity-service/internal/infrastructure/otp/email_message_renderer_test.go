package otp

import (
	"strings"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

func newTestEmailRenderer(t *testing.T) *DefaultEmailMessageRenderer {
	t.Helper()

	renderer, err := NewDefaultEmailMessageRenderer("Lenda", WithEmailCodeLifetime(5*time.Minute))
	if err != nil {
		t.Fatalf("NewDefaultEmailMessageRenderer: %v", err)
	}

	return renderer
}

func TestEmailRendererLinkEmailInEachLanguage(t *testing.T) {
	renderer := newTestEmailRenderer(t)

	tests := []struct {
		locale, subject, lead, expires, dir string
	}{
		{"en", "Confirm your email for Lenda", "confirm your email and add it to your Lenda account", "The code works for 5 minutes.", `dir="ltr"`},
		{"ar-IQ", "تأكيد بريدك الإلكتروني في Lenda", "لتأكيد بريدك الإلكتروني وربطه بحسابك في Lenda", "الرمز صالح لمدة 5 دقائق.", `dir="rtl"`},
		{"ku", "پشتڕاستکردنەوەی ئیمەیڵەکەت لە Lenda", "پشتڕاستکردنەوەی ئیمەیڵەکەت", "کۆدەکە بۆ ماوەی 5 خولەک کار دەکات.", `dir="rtl"`},
	}

	for _, test := range tests {
		t.Run(test.locale, func(t *testing.T) {
			message, err := renderer.Render(OTPEmailMessageRenderInput{
				Code:    " 123456 ",
				Purpose: auth.OTPPurposeLinkIdentifier,
				Locale:  test.locale,
			})
			if err != nil {
				t.Fatalf("Render: %v", err)
			}

			if message.Subject != test.subject {
				t.Fatalf("subject = %q, want %q", message.Subject, test.subject)
			}

			for _, body := range []string{message.TextBody, message.HTMLBody} {
				for _, want := range []string{test.lead, test.expires, "123456"} {
					if !strings.Contains(body, want) {
						t.Fatalf("body has no %q:\n%s", want, body)
					}
				}
			}

			if !strings.Contains(message.HTMLBody, test.dir) {
				t.Fatalf("HTML has no %s", test.dir)
			}

			// A rider's email is never a way to sign in.
			for _, wrong := range []string{"sign-in method", "وسيلة تسجيل دخول", "شێوازی"} {
				if strings.Contains(message.TextBody, wrong) {
					t.Fatalf("the text calls the email a sign-in method: %s", message.TextBody)
				}
			}
		})
	}
}

func TestEmailRendererEveryPurposeInEveryLanguage(t *testing.T) {
	renderer := newTestEmailRenderer(t)

	for _, locale := range []string{"en", "ar", "ku"} {
		for _, purpose := range []auth.OTPPurpose{
			auth.OTPPurposeLogin,
			auth.OTPPurposeLinkIdentifier,
			auth.OTPPurposeUnlinkIdentifier,
			auth.OTPPurposeDeleteAccount,
		} {
			message, err := renderer.Render(OTPEmailMessageRenderInput{Code: "123456", Purpose: purpose, Locale: locale})
			if err != nil {
				t.Fatalf("%s %s: %v", locale, purpose, err)
			}

			if message.Subject == "" || !strings.Contains(message.TextBody, "123456") ||
				!strings.Contains(message.HTMLBody, "123456") || strings.Contains(message.Subject, "%!") ||
				strings.Contains(message.TextBody, "%!") {
				t.Fatalf("%s %s: %+v", locale, purpose, message)
			}
		}
	}
}

func TestEmailRendererEscapesTheBrand(t *testing.T) {
	renderer, err := NewDefaultEmailMessageRenderer("<b>Ride</b>")
	if err != nil {
		t.Fatalf("NewDefaultEmailMessageRenderer: %v", err)
	}

	message, err := renderer.Render(OTPEmailMessageRenderInput{Code: "1", Purpose: auth.OTPPurposeLogin, Locale: "en"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	if strings.Contains(message.HTMLBody, "<b>Ride</b>") {
		t.Fatal("the brand went into the HTML unescaped")
	}

	// Without a lifetime the email says nothing about one.
	if strings.Contains(message.TextBody, "works for") {
		t.Fatalf("text = %q", message.TextBody)
	}
}

func TestEmailRendererArabicMinutes(t *testing.T) {
	words := emailText["ar"]

	for minutes, want := range map[int]string{
		1:  "الرمز صالح لمدة دقيقة واحدة.",
		2:  "الرمز صالح لمدة دقيقتين.",
		5:  "الرمز صالح لمدة 5 دقائق.",
		15: "الرمز صالح لمدة 15 دقيقة.",
	} {
		if got := words.expires(minutes); got != want {
			t.Fatalf("%d: %q, want %q", minutes, got, want)
		}
	}
}

func TestEmailRendererDefaultsUnsupportedLocaleToEnglish(t *testing.T) {
	message, err := newTestEmailRenderer(t).Render(OTPEmailMessageRenderInput{
		Code:    "123456",
		Purpose: auth.OTPPurposeLogin,
		Locale:  "fr-FR",
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	if message.Subject != "Your Lenda sign-in code" {
		t.Fatalf("subject = %q", message.Subject)
	}
}

func TestEmailRendererRejectsInvalidInput(t *testing.T) {
	renderer := newTestEmailRenderer(t)

	if _, err := renderer.Render(OTPEmailMessageRenderInput{Purpose: auth.OTPPurposeLogin, Locale: "en"}); err == nil {
		t.Fatal("a blank code was accepted")
	}

	if _, err := renderer.Render(OTPEmailMessageRenderInput{Code: "1", Purpose: "invalid", Locale: "en"}); err == nil {
		t.Fatal("an invalid purpose was accepted")
	}

	if _, err := NewDefaultEmailMessageRenderer("   "); err == nil {
		t.Fatal("a blank brand was accepted")
	}

	if _, err := NewDefaultEmailMessageRenderer("Lenda", WithEmailCodeLifetime(time.Second)); err == nil {
		t.Fatal("a lifetime under a minute was accepted")
	}
}

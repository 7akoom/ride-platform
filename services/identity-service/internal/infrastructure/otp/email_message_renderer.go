package otp

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"strings"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

// DefaultEmailMessageRenderer writes the code emails: a short HTML card in the
// reader's language (en, ar, ku) with the code large in the middle, and the same
// words as plain text. Email is never a rider's way in (riders sign in by
// phone); it is how staff sign in, and a rider's email is only confirmed,
// removed, or used to confirm deleting the account.
type DefaultEmailMessageRenderer struct {
	brandName string
	lifetime  time.Duration
}

type DefaultEmailMessageRendererOption func(*DefaultEmailMessageRenderer) error

// WithEmailCodeLifetime says in the email how long the code works.
func WithEmailCodeLifetime(lifetime time.Duration) DefaultEmailMessageRendererOption {
	return func(renderer *DefaultEmailMessageRenderer) error {
		if lifetime < time.Minute {
			return errors.New("email code lifetime must be at least a minute")
		}

		renderer.lifetime = lifetime

		return nil
	}
}

func NewDefaultEmailMessageRenderer(
	brandName string,
	options ...DefaultEmailMessageRendererOption,
) (*DefaultEmailMessageRenderer, error) {
	brandName = strings.TrimSpace(brandName)
	if brandName == "" {
		return nil, errors.New("email brand name is required")
	}

	renderer := &DefaultEmailMessageRenderer{brandName: brandName}

	for _, option := range options {
		if option == nil {
			return nil, errors.New("email renderer option cannot be nil")
		}

		if err := option(renderer); err != nil {
			return nil, err
		}
	}

	return renderer, nil
}

// emailWords is one language's text. Each %s is the brand name.
type emailWords struct {
	dir       string
	subject   map[auth.OTPPurpose]string
	lead      map[auth.OTPPurpose]string
	codeLabel string
	expires   func(minutes int) string
	noShare   string
	notYou    string
	footer    string
}

var emailText = map[string]emailWords{
	"en": {
		dir: "ltr",
		subject: map[auth.OTPPurpose]string{
			auth.OTPPurposeLogin:            "Your %s sign-in code",
			auth.OTPPurposeLinkIdentifier:   "Confirm your email for %s",
			auth.OTPPurposeUnlinkIdentifier: "Confirm removing your email from %s",
			auth.OTPPurposeDeleteAccount:    "Confirm deleting your %s account",
		},
		lead: map[auth.OTPPurpose]string{
			auth.OTPPurposeLogin:            "Use this code to sign in to %s.",
			auth.OTPPurposeLinkIdentifier:   "Enter this code in the app to confirm your email and add it to your %s account.",
			auth.OTPPurposeUnlinkIdentifier: "Enter this code in the app to remove this email from your %s account.",
			auth.OTPPurposeDeleteAccount:    "Enter this code in the app to confirm deleting your %s account.",
		},
		codeLabel: "Verification code",
		expires: func(minutes int) string {
			if minutes == 1 {
				return "The code works for 1 minute."
			}

			return fmt.Sprintf("The code works for %d minutes.", minutes)
		},
		noShare: "Do not share this code with anyone. The %s team will never ask you for it.",
		notYou:  "If you did not ask for this code, ignore this email; nothing changes on your account.",
		footer:  "This is an automatic email from %s. Please do not reply to it.",
	},
	"ar": {
		dir: "rtl",
		subject: map[auth.OTPPurpose]string{
			auth.OTPPurposeLogin:            "رمز الدخول إلى %s",
			auth.OTPPurposeLinkIdentifier:   "تأكيد بريدك الإلكتروني في %s",
			auth.OTPPurposeUnlinkIdentifier: "تأكيد إزالة بريدك الإلكتروني من %s",
			auth.OTPPurposeDeleteAccount:    "تأكيد حذف حسابك في %s",
		},
		lead: map[auth.OTPPurpose]string{
			auth.OTPPurposeLogin:            "استخدم هذا الرمز لتسجيل الدخول إلى %s.",
			auth.OTPPurposeLinkIdentifier:   "أدخل هذا الرمز في التطبيق لتأكيد بريدك الإلكتروني وربطه بحسابك في %s.",
			auth.OTPPurposeUnlinkIdentifier: "أدخل هذا الرمز في التطبيق لإزالة هذا البريد الإلكتروني من حسابك في %s.",
			auth.OTPPurposeDeleteAccount:    "أدخل هذا الرمز في التطبيق لتأكيد حذف حسابك في %s.",
		},
		codeLabel: "رمز التحقق",
		expires: func(minutes int) string {
			switch {
			case minutes == 1:
				return "الرمز صالح لمدة دقيقة واحدة."
			case minutes == 2:
				return "الرمز صالح لمدة دقيقتين."
			case minutes <= 10:
				return fmt.Sprintf("الرمز صالح لمدة %d دقائق.", minutes)
			default:
				return fmt.Sprintf("الرمز صالح لمدة %d دقيقة.", minutes)
			}
		},
		noShare: "لا تشارك هذا الرمز مع أي شخص، فريق %s لن يطلبه منك أبداً.",
		notYou:  "إذا لم تطلب هذا الرمز، تجاهل هذه الرسالة ولن يتغيّر شيء في حسابك.",
		footer:  "هذه رسالة تلقائية من %s، يرجى عدم الرد عليها.",
	},
	"ku": {
		dir: "rtl",
		subject: map[auth.OTPPurpose]string{
			auth.OTPPurposeLogin:            "کۆدی چوونەژوورەوە بۆ %s",
			auth.OTPPurposeLinkIdentifier:   "پشتڕاستکردنەوەی ئیمەیڵەکەت لە %s",
			auth.OTPPurposeUnlinkIdentifier: "پشتڕاستکردنەوەی لابردنی ئیمەیڵەکەت لە %s",
			auth.OTPPurposeDeleteAccount:    "پشتڕاستکردنەوەی سڕینەوەی هەژمارەکەت لە %s",
		},
		lead: map[auth.OTPPurpose]string{
			auth.OTPPurposeLogin:            "ئەم کۆدە بەکاربهێنە بۆ چوونەژوورەوە بۆ %s.",
			auth.OTPPurposeLinkIdentifier:   "ئەم کۆدە لە ئەپەکەدا بنووسە بۆ پشتڕاستکردنەوەی ئیمەیڵەکەت و بەستنەوەی بە هەژمارەکەتەوە لە %s.",
			auth.OTPPurposeUnlinkIdentifier: "ئەم کۆدە لە ئەپەکەدا بنووسە بۆ لابردنی ئەم ئیمەیڵە لە هەژمارەکەت لە %s.",
			auth.OTPPurposeDeleteAccount:    "ئەم کۆدە لە ئەپەکەدا بنووسە بۆ پشتڕاستکردنەوەی سڕینەوەی هەژمارەکەت لە %s.",
		},
		codeLabel: "کۆدی پشتڕاستکردنەوە",
		expires: func(minutes int) string {
			return fmt.Sprintf("کۆدەکە بۆ ماوەی %d خولەک کار دەکات.", minutes)
		},
		noShare: "ئەم کۆدە لەگەڵ هیچ کەسێک هاوبەش مەکە، تیمی %s هەرگیز داوای ناکات.",
		notYou:  "ئەگەر داوای ئەم کۆدەت نەکردووە، گوێی پێ مەدە؛ هیچ شتێک لە هەژمارەکەتدا ناگۆڕێت.",
		footer:  "ئەمە ئیمەیڵێکی خۆکارە لە %s، تکایە وەڵامی مەدەرەوە.",
	},
}

type emailPage struct {
	Lang, Dir, Align, Brand, Subject, Lead, CodeLabel, Code, Expires, NoShare, NotYou, Footer string
}

func (r *DefaultEmailMessageRenderer) Render(
	input OTPEmailMessageRenderInput,
) (RenderedEmailMessage, error) {
	code := strings.TrimSpace(input.Code)
	if code == "" {
		return RenderedEmailMessage{}, errors.New("OTP code is required")
	}

	purpose, err := auth.ParseOTPPurpose(string(input.Purpose))
	if err != nil {
		return RenderedEmailMessage{}, fmt.Errorf("validate OTP email purpose: %w", err)
	}

	locale := normalizeOTPMessageLocale(input.Locale)

	words, ok := emailText[locale]
	if !ok {
		locale, words = "en", emailText["en"]
	}

	subject, ok := words.subject[purpose]
	if !ok {
		return RenderedEmailMessage{}, auth.ErrInvalidOTPPurpose
	}

	page := emailPage{
		Lang:      locale,
		Dir:       words.dir,
		Align:     map[string]string{"rtl": "right", "ltr": "left"}[words.dir],
		Brand:     r.brandName,
		Subject:   fmt.Sprintf(subject, r.brandName),
		Lead:      fmt.Sprintf(words.lead[purpose], r.brandName),
		CodeLabel: words.codeLabel,
		Code:      code,
		NoShare:   fmt.Sprintf(words.noShare, r.brandName),
		NotYou:    words.notYou,
		Footer:    fmt.Sprintf(words.footer, r.brandName),
	}

	if r.lifetime > 0 {
		page.Expires = words.expires(int(r.lifetime / time.Minute))
	}

	var htmlBody bytes.Buffer
	if err := emailTemplate.Execute(&htmlBody, page); err != nil {
		return RenderedEmailMessage{}, fmt.Errorf("render OTP email: %w", err)
	}

	lines := []string{page.Lead, "", page.CodeLabel + ": " + code, ""}
	if page.Expires != "" {
		lines = append(lines, page.Expires)
	}

	lines = append(lines, page.NoShare, "", page.NotYou, "", "— "+page.Footer)

	return RenderedEmailMessage{
		Subject:  page.Subject,
		TextBody: strings.Join(lines, "\n"),
		HTMLBody: htmlBody.String(),
	}, nil
}

// emailTemplate is a table layout with inline styles, the form mail apps
// (Gmail, Outlook, Apple Mail) all show the same way.
var emailTemplate = template.Must(template.New("otp-email").Parse(`<!doctype html>
<html lang="{{.Lang}}" dir="{{.Dir}}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="light">
<title>{{.Subject}}</title>
</head>
<body style="margin:0;padding:0;background:#F4F4F5;">
<div style="display:none;max-height:0;overflow:hidden;">{{.Lead}}</div>
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background:#F4F4F5;">
<tr><td align="center" style="padding:32px 16px;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" dir="{{.Dir}}" style="max-width:480px;background:#FFFFFF;border-radius:16px;overflow:hidden;font-family:-apple-system,'Segoe UI',Tahoma,Arial,sans-serif;color:#05070F;">
<tr><td style="background:#05070F;padding:20px 28px;text-align:{{.Align}};">
<span style="font-size:20px;font-weight:700;color:#F3D59A;letter-spacing:0.3px;">{{.Brand}}</span>
</td></tr>
<tr><td style="padding:28px 28px 8px;text-align:{{.Align}};">
<h1 style="margin:0 0 12px;font-size:20px;line-height:1.4;font-weight:700;color:#05070F;">{{.Subject}}</h1>
<p style="margin:0;font-size:15px;line-height:1.7;color:#3F4152;">{{.Lead}}</p>
</td></tr>
<tr><td align="center" style="padding:20px 28px;">
<div style="font-size:13px;color:#6B6E80;margin-bottom:8px;">{{.CodeLabel}}</div>
<div dir="ltr" style="display:inline-block;background:#FBF4E4;border:1px solid #F3D59A;border-radius:12px;padding:14px 14px 14px 24px;font-family:'SF Mono',Menlo,Consolas,'Courier New',monospace;font-size:34px;font-weight:700;letter-spacing:10px;color:#05070F;">{{.Code}}</div>
{{if .Expires}}<div style="font-size:13px;color:#6B6E80;margin-top:10px;">{{.Expires}}</div>{{end}}
</td></tr>
<tr><td style="padding:8px 28px 28px;text-align:{{.Align}};">
<p style="margin:0 0 10px;font-size:14px;line-height:1.7;color:#3F4152;"><strong style="color:#05070F;">{{.NoShare}}</strong></p>
<p style="margin:0;font-size:13px;line-height:1.7;color:#6B6E80;">{{.NotYou}}</p>
</td></tr>
<tr><td style="border-top:1px solid #EEEEF1;padding:16px 28px;text-align:{{.Align}};font-size:12px;line-height:1.6;color:#9A9CAB;">{{.Footer}}</td></tr>
</table>
</td></tr>
</table>
</body>
</html>
`))

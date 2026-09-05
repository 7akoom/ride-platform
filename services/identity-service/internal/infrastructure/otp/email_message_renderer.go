package otp

import (
	"errors"
	"fmt"
	"html"
	"strings"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

type DefaultEmailMessageRenderer struct {
	brandName string
}

func NewDefaultEmailMessageRenderer(
	brandName string,
) (*DefaultEmailMessageRenderer, error) {
	brandName = strings.TrimSpace(
		brandName,
	)
	if brandName == "" {
		return nil, errors.New(
			"email brand name is required",
		)
	}

	return &DefaultEmailMessageRenderer{
		brandName: brandName,
	}, nil
}

func (r *DefaultEmailMessageRenderer) Render(
	input OTPEmailMessageRenderInput,
) (RenderedEmailMessage, error) {
	code := strings.TrimSpace(
		input.Code,
	)
	if code == "" {
		return RenderedEmailMessage{},
			errors.New(
				"OTP code is required",
			)
	}

	purpose, err := auth.ParseOTPPurpose(
		string(input.Purpose),
	)
	if err != nil {
		return RenderedEmailMessage{},
			fmt.Errorf(
				"validate OTP email purpose: %w",
				err,
			)
	}

	locale := normalizeOTPMessageLocale(
		input.Locale,
	)

	var (
		subject   string
		textBody  string
		direction string
	)

	switch locale {
	case "ar":
		subject, textBody, err =
			r.renderArabicEmail(
				code,
				purpose,
			)
		direction = "rtl"

	case "ku":
		subject, textBody, err =
			r.renderKurdishEmail(
				code,
				purpose,
			)
		direction = "rtl"

	default:
		subject, textBody, err =
			r.renderEnglishEmail(
				code,
				purpose,
			)
		direction = "ltr"
	}

	if err != nil {
		return RenderedEmailMessage{},
			err
	}

	htmlBody := fmt.Sprintf(
		`<div dir="%s"><p>%s</p></div>`,
		direction,
		html.EscapeString(textBody),
	)

	return RenderedEmailMessage{
		Subject:  subject,
		TextBody: textBody,
		HTMLBody: htmlBody,
	}, nil
}

func (r *DefaultEmailMessageRenderer) renderEnglishEmail(
	code string,
	purpose auth.OTPPurpose,
) (string, string, error) {
	switch purpose {
	case auth.OTPPurposeLogin:
		return fmt.Sprintf(
				"%s verification code",
				r.brandName,
			),
			fmt.Sprintf(
				"%s verification code: %s. Do not share this code with anyone.",
				r.brandName,
				code,
			),
			nil

	case auth.OTPPurposeLinkIdentifier:
		return fmt.Sprintf(
				"%s sign-in method verification",
				r.brandName,
			),
			fmt.Sprintf(
				"Use code %s to verify a new sign-in method for %s. Do not share this code with anyone.",
				code,
				r.brandName,
			),
			nil

	case auth.OTPPurposeUnlinkIdentifier:
		return fmt.Sprintf(
				"%s sign-in method removal",
				r.brandName,
			),
			fmt.Sprintf(
				"Use code %s to remove a sign-in method from %s. Do not share this code with anyone.",
				code,
				r.brandName,
			),
			nil

	default:
		return "", "", auth.ErrInvalidOTPPurpose
	}
}

func (r *DefaultEmailMessageRenderer) renderArabicEmail(
	code string,
	purpose auth.OTPPurpose,
) (string, string, error) {
	switch purpose {
	case auth.OTPPurposeLogin:
		return fmt.Sprintf(
				"رمز التحقق من %s",
				r.brandName,
			),
			fmt.Sprintf(
				"رمز التحقق الخاص بـ %s هو %s. لا تشارك هذا الرمز مع أي شخص.",
				r.brandName,
				code,
			),
			nil

	case auth.OTPPurposeLinkIdentifier:
		return fmt.Sprintf(
				"تأكيد وسيلة تسجيل دخول جديدة إلى %s",
				r.brandName,
			),
			fmt.Sprintf(
				"استخدم الرمز %s لتأكيد وسيلة تسجيل دخول جديدة إلى %s. لا تشارك هذا الرمز مع أي شخص.",
				code,
				r.brandName,
			),
			nil

	case auth.OTPPurposeUnlinkIdentifier:
		return fmt.Sprintf(
				"إزالة وسيلة تسجيل دخول من %s",
				r.brandName,
			),
			fmt.Sprintf(
				"استخدم الرمز %s لإزالة وسيلة تسجيل دخول من %s. لا تشارك هذا الرمز مع أي شخص.",
				code,
				r.brandName,
			),
			nil

	default:
		return "", "", auth.ErrInvalidOTPPurpose
	}
}

func (r *DefaultEmailMessageRenderer) renderKurdishEmail(
	code string,
	purpose auth.OTPPurpose,
) (string, string, error) {
	switch purpose {
	case auth.OTPPurposeLogin:
		return fmt.Sprintf(
				"کۆدی پشتڕاستکردنەوەی %s",
				r.brandName,
			),
			fmt.Sprintf(
				"کۆدی پشتڕاستکردنەوەی %s بریتییە لە %s. ئەم کۆدە لەگەڵ هیچ کەسێک هاوبەش مەکە.",
				r.brandName,
				code,
			),
			nil

	case auth.OTPPurposeLinkIdentifier:
		return fmt.Sprintf(
				"پشتڕاستکردنەوەی شێوازی نوێی چوونەژوورەوەی %s",
				r.brandName,
			),
			fmt.Sprintf(
				"کۆدی %s بەکاربهێنە بۆ پشتڕاستکردنەوەی شێوازی نوێی چوونەژوورەوەی %s. ئەم کۆدە لەگەڵ هیچ کەسێک هاوبەش مەکە.",
				code,
				r.brandName,
			),
			nil

	case auth.OTPPurposeUnlinkIdentifier:
		return fmt.Sprintf(
				"لابردنی شێوازی چوونەژوورەوەی %s",
				r.brandName,
			),
			fmt.Sprintf(
				"کۆدی %s بەکاربهێنە بۆ لابردنی شێوازی چوونەژوورەوەی %s. ئەم کۆدە لەگەڵ هیچ کەسێک هاوبەش مەکە.",
				code,
				r.brandName,
			),
			nil

	default:
		return "", "", auth.ErrInvalidOTPPurpose
	}
}

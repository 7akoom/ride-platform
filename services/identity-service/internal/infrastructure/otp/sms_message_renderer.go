package otp

import (
	"errors"
	"fmt"
	"strings"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

type DefaultSMSMessageRenderer struct {
	brandName string
}

func NewDefaultSMSMessageRenderer(
	brandName string,
) (*DefaultSMSMessageRenderer, error) {
	brandName = strings.TrimSpace(brandName)

	if brandName == "" {
		return nil, errors.New(
			"SMS brand name is required",
		)
	}

	return &DefaultSMSMessageRenderer{
		brandName: brandName,
	}, nil
}

func (r *DefaultSMSMessageRenderer) Render(
	input OTPMessageRenderInput,
) (string, error) {
	code := strings.TrimSpace(
		input.Code,
	)

	if code == "" {
		return "", errors.New(
			"OTP code is required",
		)
	}

	purpose, err := auth.ParseOTPPurpose(
		string(input.Purpose),
	)
	if err != nil {
		return "", fmt.Errorf(
			"validate OTP message purpose: %w",
			err,
		)
	}

	switch normalizeOTPMessageLocale(input.Locale) {
	case "ar":
		return r.renderArabicSMS(
			code,
			purpose,
		)

	case "ku":
		return r.renderKurdishSMS(
			code,
			purpose,
		)

	default:
		return r.renderEnglishSMS(
			code,
			purpose,
		)
	}
}

func (r *DefaultSMSMessageRenderer) renderEnglishSMS(
	code string,
	purpose auth.OTPPurpose,
) (string, error) {
	switch purpose {
	case auth.OTPPurposeLogin:
		return fmt.Sprintf(
			"%s verification code: %s. Do not share this code.",
			r.brandName,
			code,
		), nil

	case auth.OTPPurposeLinkIdentifier:
		return fmt.Sprintf(
			"%s code to verify a new sign-in method: %s. Do not share this code.",
			r.brandName,
			code,
		), nil

	case auth.OTPPurposeUnlinkIdentifier:
		return fmt.Sprintf(
			"%s code to remove a sign-in method: %s. Do not share this code.",
			r.brandName,
			code,
		), nil

	default:
		return "", auth.ErrInvalidOTPPurpose
	}
}

func (r *DefaultSMSMessageRenderer) renderArabicSMS(
	code string,
	purpose auth.OTPPurpose,
) (string, error) {
	switch purpose {
	case auth.OTPPurposeLogin:
		return fmt.Sprintf(
			"رمز التحقق الخاص بـ %s هو %s. لا تشارك هذا الرمز مع أي شخص.",
			r.brandName,
			code,
		), nil

	case auth.OTPPurposeLinkIdentifier:
		return fmt.Sprintf(
			"رمز %s لتأكيد وسيلة تسجيل دخول جديدة هو %s. لا تشارك هذا الرمز مع أي شخص.",
			r.brandName,
			code,
		), nil

	case auth.OTPPurposeUnlinkIdentifier:
		return fmt.Sprintf(
			"رمز %s لإزالة وسيلة تسجيل دخول هو %s. لا تشارك هذا الرمز مع أي شخص.",
			r.brandName,
			code,
		), nil

	default:
		return "", auth.ErrInvalidOTPPurpose
	}
}

func (r *DefaultSMSMessageRenderer) renderKurdishSMS(
	code string,
	purpose auth.OTPPurpose,
) (string, error) {
	switch purpose {
	case auth.OTPPurposeLogin:
		return fmt.Sprintf(
			"کۆدی پشتڕاستکردنەوەی %s: %s. ئەم کۆدە لەگەڵ کەسێک هاوبەش مەکە.",
			r.brandName,
			code,
		), nil

	case auth.OTPPurposeLinkIdentifier:
		return fmt.Sprintf(
			"کۆدی %s بۆ پشتڕاستکردنەوەی شێوازی نوێی چوونەژوورەوە: %s. ئەم کۆدە لەگەڵ کەسێک هاوبەش مەکە.",
			r.brandName,
			code,
		), nil

	case auth.OTPPurposeUnlinkIdentifier:
		return fmt.Sprintf(
			"کۆدی %s بۆ سڕینەوەی شێوازی چوونەژوورەوە: %s. ئەم کۆدە لەگەڵ کەسێک هاوبەش مەکە.",
			r.brandName,
			code,
		), nil

	default:
		return "", auth.ErrInvalidOTPPurpose
	}
}

func normalizeOTPMessageLocale(
	value string,
) string {
	normalized := strings.ToLower(
		strings.TrimSpace(value),
	)

	normalized = strings.ReplaceAll(
		normalized,
		"_",
		"-",
	)

	switch {
	case normalized == "ar" ||
		strings.HasPrefix(normalized, "ar-"):
		return "ar"

	case normalized == "ku" ||
		strings.HasPrefix(normalized, "ku-"):
		return "ku"

	case normalized == "en" ||
		strings.HasPrefix(normalized, "en-"):
		return "en"

	default:
		return "en"
	}
}

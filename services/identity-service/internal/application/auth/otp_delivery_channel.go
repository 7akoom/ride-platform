package auth

import (
	"errors"
	"strings"
)

type OTPDeliveryChannel string

const (
	OTPDeliveryChannelAuto     OTPDeliveryChannel = "auto"
	OTPDeliveryChannelSMS      OTPDeliveryChannel = "sms"
	OTPDeliveryChannelWhatsApp OTPDeliveryChannel = "whatsapp"
	OTPDeliveryChannelEmail    OTPDeliveryChannel = "email"
)

var ErrInvalidOTPDeliveryChannel = errors.New(
	"invalid OTP delivery channel",
)

func ParseOTPDeliveryChannel(
	value string,
) (OTPDeliveryChannel, error) {
	normalized := strings.ToLower(
		strings.TrimSpace(value),
	)

	switch OTPDeliveryChannel(normalized) {
	case "":
		return OTPDeliveryChannelAuto, nil

	case OTPDeliveryChannelAuto:
		return OTPDeliveryChannelAuto, nil

	case OTPDeliveryChannelSMS:
		return OTPDeliveryChannelSMS, nil

	case OTPDeliveryChannelWhatsApp:
		return OTPDeliveryChannelWhatsApp, nil

	case OTPDeliveryChannelEmail:
		return OTPDeliveryChannelEmail, nil

	default:
		return "", ErrInvalidOTPDeliveryChannel
	}
}

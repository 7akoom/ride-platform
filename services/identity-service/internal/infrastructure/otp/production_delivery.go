package otp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

type SMSSender interface {
	Send(
		ctx context.Context,
		phoneNumber string,
		code string,
		purpose auth.OTPPurpose,
		locale string,
	) error
}

type WhatsAppSender interface {
	Send(
		ctx context.Context,
		phoneNumber string,
		code string,
		purpose auth.OTPPurpose,
		locale string,
	) error
}

type EmailSender interface {
	Send(
		ctx context.Context,
		emailAddress string,
		code string,
		purpose auth.OTPPurpose,
		locale string,
	) error
}

type ProductionDelivery struct {
	smsSender      SMSSender
	whatsAppSender WhatsAppSender
	emailSender    EmailSender

	// phoneAutoChannel is where a phone code goes when the client asks for
	// "auto": SMS (the default) or WhatsApp.
	phoneAutoChannel auth.OTPDeliveryChannel
}

// ProductionDeliveryOption adjusts a ProductionDelivery.
type ProductionDeliveryOption func(*ProductionDelivery) error

// WithPhoneAutoChannel sends "auto" phone codes by SMS or by WhatsApp.
func WithPhoneAutoChannel(
	channel auth.OTPDeliveryChannel,
) ProductionDeliveryOption {
	return func(d *ProductionDelivery) error {
		switch channel {
		case auth.OTPDeliveryChannelSMS,
			auth.OTPDeliveryChannelWhatsApp:
			d.phoneAutoChannel = channel

			return nil

		default:
			return fmt.Errorf(
				"phone auto channel must be sms or whatsapp, got %q",
				channel,
			)
		}
	}
}

// NewProductionDelivery needs an email sender and at least one phone
// sender; the phone channel "auto" uses must have its sender. A missing
// SMS or WhatsApp sender makes that channel unavailable, not an error.
func NewProductionDelivery(
	smsSender SMSSender,
	whatsAppSender WhatsAppSender,
	emailSender EmailSender,
	options ...ProductionDeliveryOption,
) (*ProductionDelivery, error) {
	if smsSender == nil && whatsAppSender == nil {
		return nil, errors.New(
			"an SMS or a WhatsApp sender is required",
		)
	}

	if emailSender == nil {
		return nil, errors.New(
			"email sender is required",
		)
	}

	delivery := &ProductionDelivery{
		smsSender:        smsSender,
		whatsAppSender:   whatsAppSender,
		emailSender:      emailSender,
		phoneAutoChannel: auth.OTPDeliveryChannelSMS,
	}

	for _, option := range options {
		if option == nil {
			return nil, errors.New(
				"production delivery option cannot be nil",
			)
		}

		if err := option(delivery); err != nil {
			return nil, err
		}
	}

	switch delivery.phoneAutoChannel {
	case auth.OTPDeliveryChannelSMS:
		if smsSender == nil {
			return nil, errors.New(
				"SMS sender is required when phone codes go by SMS by default",
			)
		}

	case auth.OTPDeliveryChannelWhatsApp:
		if whatsAppSender == nil {
			return nil, errors.New(
				"WhatsApp sender is required when phone codes go by WhatsApp by default",
			)
		}
	}

	return delivery, nil
}

func (d *ProductionDelivery) Send(
	ctx context.Context,
	input auth.OTPDeliveryInput,
) error {
	identifier, err := auth.NewIdentifier(
		input.Identifier.Type,
		input.Identifier.Value,
	)
	if err != nil {
		return fmt.Errorf(
			"validate OTP delivery identifier: %w",
			err,
		)
	}

	code := strings.TrimSpace(
		input.Code,
	)
	if code == "" {
		return errors.New(
			"OTP delivery code is required",
		)
	}

	purpose, err := auth.ParseOTPPurpose(
		string(input.Purpose),
	)
	if err != nil {
		return fmt.Errorf(
			"validate OTP delivery purpose: %w",
			err,
		)
	}

	channel, err := auth.ParseOTPDeliveryChannel(
		string(input.Channel),
	)
	if err != nil {
		return fmt.Errorf(
			"validate OTP delivery channel: %w",
			err,
		)
	}

	switch channel {
	case auth.OTPDeliveryChannelAuto:
		switch identifier.Type {
		case auth.IdentifierTypePhone:
			if d.phoneAutoChannel == auth.OTPDeliveryChannelWhatsApp {
				return d.sendWhatsApp(
					ctx,
					input.ChallengeID,
					identifier.Value,
					code,
					purpose,
					input.Locale,
				)
			}

			return d.sendSMS(
				ctx,
				input.ChallengeID,
				identifier.Value,
				code,
				purpose,
				input.Locale,
			)

		case auth.IdentifierTypeEmail:
			return d.sendEmail(
				ctx,
				identifier.Value,
				code,
				purpose,
				input.Locale,
			)

		default:
			return auth.ErrInvalidIdentifierType
		}

	case auth.OTPDeliveryChannelSMS:
		if identifier.Type != auth.IdentifierTypePhone {
			return fmt.Errorf(
				"%w: SMS requires a phone identifier",
				auth.ErrInvalidOTPDeliveryChannel,
			)
		}

		return d.sendSMS(
			ctx,
			input.ChallengeID,
			identifier.Value,
			code,
			purpose,
			input.Locale,
		)

	case auth.OTPDeliveryChannelWhatsApp:
		if identifier.Type != auth.IdentifierTypePhone {
			return fmt.Errorf(
				"%w: WhatsApp requires a phone identifier",
				auth.ErrInvalidOTPDeliveryChannel,
			)
		}

		return d.sendWhatsApp(
			ctx,
			input.ChallengeID,
			identifier.Value,
			code,
			purpose,
			input.Locale,
		)

	case auth.OTPDeliveryChannelEmail:
		if identifier.Type != auth.IdentifierTypeEmail {
			return fmt.Errorf(
				"%w: email requires an email identifier",
				auth.ErrInvalidOTPDeliveryChannel,
			)
		}

		return d.sendEmail(
			ctx,
			identifier.Value,
			code,
			purpose,
			input.Locale,
		)

	default:
		return auth.ErrInvalidOTPDeliveryChannel
	}
}

func (d *ProductionDelivery) sendSMS(
	ctx context.Context,
	challengeID string,
	phoneNumber string,
	code string,
	purpose auth.OTPPurpose,
	locale string,
) error {
	if d.smsSender == nil {
		return fmt.Errorf(
			"%w: SMS sender is not configured",
			auth.ErrOTPDeliveryChannelUnavailable,
		)
	}

	if challengeAwareSender, ok :=
		d.smsSender.(ChallengeAwareSMSSender); ok &&
		strings.TrimSpace(challengeID) != "" {
		if err := challengeAwareSender.SendForChallenge(
			ctx,
			challengeID,
			phoneNumber,
			code,
			purpose,
			locale,
		); err != nil {
			return fmt.Errorf(
				"send OTP by SMS: %w",
				err,
			)
		}

		return nil
	}

	if err := d.smsSender.Send(
		ctx,
		phoneNumber,
		code,
		purpose,
		locale,
	); err != nil {
		return fmt.Errorf(
			"send OTP by SMS: %w",
			err,
		)
	}

	return nil
}

func (d *ProductionDelivery) sendWhatsApp(
	ctx context.Context,
	challengeID string,
	phoneNumber string,
	code string,
	purpose auth.OTPPurpose,
	locale string,
) error {
	if d.whatsAppSender == nil {
		return fmt.Errorf(
			"%w: WhatsApp sender is not configured",
			auth.ErrOTPDeliveryChannelUnavailable,
		)
	}

	if challengeAwareSender, ok :=
		d.whatsAppSender.(ChallengeAwareWhatsAppSender); ok &&
		strings.TrimSpace(challengeID) != "" {
		if err := challengeAwareSender.SendForChallenge(
			ctx,
			challengeID,
			phoneNumber,
			code,
			purpose,
			locale,
		); err != nil {
			return fmt.Errorf(
				"send OTP by WhatsApp: %w",
				err,
			)
		}

		return nil
	}

	if err := d.whatsAppSender.Send(
		ctx,
		phoneNumber,
		code,
		purpose,
		locale,
	); err != nil {
		return fmt.Errorf(
			"send OTP by WhatsApp: %w",
			err,
		)
	}

	return nil
}

func (d *ProductionDelivery) sendEmail(
	ctx context.Context,
	emailAddress string,
	code string,
	purpose auth.OTPPurpose,
	locale string,
) error {
	if err := d.emailSender.Send(
		ctx,
		emailAddress,
		code,
		purpose,
		locale,
	); err != nil {
		return fmt.Errorf(
			"send OTP by email: %w",
			err,
		)
	}

	return nil
}

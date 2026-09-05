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
}

func NewProductionDelivery(
	smsSender SMSSender,
	whatsAppSender WhatsAppSender,
	emailSender EmailSender,
) (*ProductionDelivery, error) {
	if smsSender == nil {
		return nil, errors.New(
			"SMS sender is required",
		)
	}

	if emailSender == nil {
		return nil, errors.New(
			"email sender is required",
		)
	}

	return &ProductionDelivery{
		smsSender:      smsSender,
		whatsAppSender: whatsAppSender,
		emailSender:    emailSender,
	}, nil
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

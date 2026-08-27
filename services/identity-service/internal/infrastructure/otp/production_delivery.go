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
	smsSender   SMSSender
	emailSender EmailSender
}

func NewProductionDelivery(
	smsSender SMSSender,
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
		smsSender:   smsSender,
		emailSender: emailSender,
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

	switch identifier.Type {
	case auth.IdentifierTypePhone:
		if err := d.smsSender.Send(
			ctx,
			identifier.Value,
			code,
			purpose,
			input.Locale,
		); err != nil {
			return fmt.Errorf(
				"send OTP by SMS: %w",
				err,
			)
		}

	case auth.IdentifierTypeEmail:
		if err := d.emailSender.Send(
			ctx,
			identifier.Value,
			code,
			purpose,
			input.Locale,
		); err != nil {
			return fmt.Errorf(
				"send OTP by email: %w",
				err,
			)
		}

	default:
		return auth.ErrInvalidIdentifierType
	}

	return nil
}

package otp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

type WhatsAppOTPProviderInput struct {
	ChallengeID string
	PhoneNumber string
	Code        string
	Purpose     auth.OTPPurpose
	Locale      string
}

type WhatsAppProvider interface {
	SendOTP(
		ctx context.Context,
		input WhatsAppOTPProviderInput,
	) error
}

type WhatsAppProviderDeliveryResult struct {
	ProviderMessageID string
	ProviderStatus    string
}

type TrackedWhatsAppProvider interface {
	SendOTPTracked(
		ctx context.Context,
		input WhatsAppOTPProviderInput,
	) (WhatsAppProviderDeliveryResult, error)
}

type ChallengeAwareWhatsAppSender interface {
	SendForChallenge(
		ctx context.Context,
		challengeID string,
		phoneNumber string,
		code string,
		purpose auth.OTPPurpose,
		locale string,
	) error
}

type ProviderWhatsAppSender struct {
	provider WhatsAppProvider
}

func NewProviderWhatsAppSender(
	provider WhatsAppProvider,
) (*ProviderWhatsAppSender, error) {
	if provider == nil {
		return nil, errors.New(
			"WhatsApp provider is required",
		)
	}

	return &ProviderWhatsAppSender{
		provider: provider,
	}, nil
}

func (s *ProviderWhatsAppSender) Send(
	ctx context.Context,
	phoneNumber string,
	code string,
	purpose auth.OTPPurpose,
	locale string,
) error {
	return s.send(
		ctx,
		"",
		phoneNumber,
		code,
		purpose,
		locale,
	)
}

func (s *ProviderWhatsAppSender) SendForChallenge(
	ctx context.Context,
	challengeID string,
	phoneNumber string,
	code string,
	purpose auth.OTPPurpose,
	locale string,
) error {
	challengeID = strings.TrimSpace(
		challengeID,
	)
	if challengeID == "" {
		return errors.New(
			"WhatsApp challenge ID is required",
		)
	}

	return s.send(
		ctx,
		challengeID,
		phoneNumber,
		code,
		purpose,
		locale,
	)
}

func (s *ProviderWhatsAppSender) send(
	ctx context.Context,
	challengeID string,
	phoneNumber string,
	code string,
	purpose auth.OTPPurpose,
	locale string,
) error {
	phoneNumber = strings.TrimSpace(
		phoneNumber,
	)
	if phoneNumber == "" {
		return errors.New(
			"WhatsApp phone number is required",
		)
	}

	code = strings.TrimSpace(
		code,
	)
	if code == "" {
		return errors.New(
			"WhatsApp OTP code is required",
		)
	}

	parsedPurpose, err := auth.ParseOTPPurpose(
		string(purpose),
	)
	if err != nil {
		return fmt.Errorf(
			"validate WhatsApp OTP purpose: %w",
			err,
		)
	}

	if err := s.provider.SendOTP(
		ctx,
		WhatsAppOTPProviderInput{
			ChallengeID: challengeID,
			PhoneNumber: phoneNumber,
			Code:        code,
			Purpose:     parsedPurpose,
			Locale: strings.TrimSpace(
				locale,
			),
		},
	); err != nil {
		return fmt.Errorf(
			"send WhatsApp OTP through provider: %w",
			err,
		)
	}

	return nil
}

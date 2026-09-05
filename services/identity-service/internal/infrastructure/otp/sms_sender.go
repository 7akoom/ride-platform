package otp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

type SMSMessage struct {
	ChallengeID string
	To          string
	Body        string
	Code        string
	Locale      string
}

type SMSProvider interface {
	Send(
		ctx context.Context,
		message SMSMessage,
	) error
}

type SMSProviderDeliveryResult struct {
	ProviderMessageID string
	ProviderStatus    string
}

type TrackedSMSProvider interface {
	SendTracked(
		ctx context.Context,
		message SMSMessage,
	) (SMSProviderDeliveryResult, error)
}

type ChallengeAwareSMSSender interface {
	SendForChallenge(
		ctx context.Context,
		challengeID string,
		phoneNumber string,
		code string,
		purpose auth.OTPPurpose,
		locale string,
	) error
}

type OTPMessageRenderInput struct {
	Code    string
	Purpose auth.OTPPurpose
	Locale  string
}

type SMSMessageRenderer interface {
	Render(
		input OTPMessageRenderInput,
	) (string, error)
}

type ProviderSMSSender struct {
	provider SMSProvider
	renderer SMSMessageRenderer
}

func NewProviderSMSSender(
	provider SMSProvider,
	renderer SMSMessageRenderer,
) (*ProviderSMSSender, error) {
	if provider == nil {
		return nil, errors.New(
			"SMS provider is required",
		)
	}

	if renderer == nil {
		return nil, errors.New(
			"SMS message renderer is required",
		)
	}

	return &ProviderSMSSender{
		provider: provider,
		renderer: renderer,
	}, nil
}

func (s *ProviderSMSSender) Send(
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

func (s *ProviderSMSSender) SendForChallenge(
	ctx context.Context,
	challengeID string,
	phoneNumber string,
	code string,
	purpose auth.OTPPurpose,
	locale string,
) error {
	challengeID = strings.TrimSpace(challengeID)
	if challengeID == "" {
		return errors.New(
			"SMS challenge ID is required",
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

func (s *ProviderSMSSender) send(
	ctx context.Context,
	challengeID string,
	phoneNumber string,
	code string,
	purpose auth.OTPPurpose,
	locale string,
) error {
	phoneNumber = strings.TrimSpace(phoneNumber)
	if phoneNumber == "" {
		return errors.New(
			"SMS phone number is required",
		)
	}

	code = strings.TrimSpace(code)
	if code == "" {
		return errors.New(
			"SMS OTP code is required",
		)
	}

	body, err := s.renderer.Render(
		OTPMessageRenderInput{
			Code:    code,
			Purpose: purpose,
			Locale:  locale,
		},
	)
	if err != nil {
		return fmt.Errorf(
			"render SMS message: %w",
			err,
		)
	}

	body = strings.TrimSpace(body)
	if body == "" {
		return errors.New(
			"rendered SMS message is empty",
		)
	}

	if err := s.provider.Send(
		ctx,
		SMSMessage{
			ChallengeID: challengeID,
			To:          phoneNumber,
			Body:        body,
			Code:        code,
			Locale:      locale,
		},
	); err != nil {
		return fmt.Errorf(
			"send SMS through provider: %w",
			err,
		)
	}

	return nil
}

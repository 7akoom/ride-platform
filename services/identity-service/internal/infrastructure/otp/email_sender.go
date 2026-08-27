package otp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

type EmailMessage struct {
	To       string
	Subject  string
	TextBody string
	HTMLBody string
}

type EmailProvider interface {
	Send(
		ctx context.Context,
		message EmailMessage,
	) error
}

type OTPEmailMessageRenderInput struct {
	Code    string
	Purpose auth.OTPPurpose
	Locale  string
}

type RenderedEmailMessage struct {
	Subject  string
	TextBody string
	HTMLBody string
}

type EmailMessageRenderer interface {
	Render(
		input OTPEmailMessageRenderInput,
	) (RenderedEmailMessage, error)
}

type ProviderEmailSender struct {
	provider EmailProvider
	renderer EmailMessageRenderer
}

func NewProviderEmailSender(
	provider EmailProvider,
	renderer EmailMessageRenderer,
) (*ProviderEmailSender, error) {
	if provider == nil {
		return nil, errors.New(
			"email provider is required",
		)
	}

	if renderer == nil {
		return nil, errors.New(
			"email message renderer is required",
		)
	}

	return &ProviderEmailSender{
		provider: provider,
		renderer: renderer,
	}, nil
}

func (s *ProviderEmailSender) Send(
	ctx context.Context,
	emailAddress string,
	code string,
	purpose auth.OTPPurpose,
	locale string,
) error {
	identifier, err := auth.NewIdentifier(
		auth.IdentifierTypeEmail,
		emailAddress,
	)
	if err != nil {
		return fmt.Errorf(
			"validate OTP email address: %w",
			err,
		)
	}

	code = strings.TrimSpace(code)
	if code == "" {
		return errors.New(
			"OTP email code is required",
		)
	}

	purpose, err = auth.ParseOTPPurpose(
		string(purpose),
	)
	if err != nil {
		return fmt.Errorf(
			"validate OTP email purpose: %w",
			err,
		)
	}

	renderedMessage, err := s.renderer.Render(
		OTPEmailMessageRenderInput{
			Code:    code,
			Purpose: purpose,
			Locale:  locale,
		},
	)
	if err != nil {
		return fmt.Errorf(
			"render OTP email message: %w",
			err,
		)
	}

	subject := strings.TrimSpace(
		renderedMessage.Subject,
	)
	if subject == "" {
		return errors.New(
			"OTP email subject is required",
		)
	}

	textBody := strings.TrimSpace(
		renderedMessage.TextBody,
	)
	if textBody == "" {
		return errors.New(
			"OTP email text body is required",
		)
	}

	if err := s.provider.Send(
		ctx,
		EmailMessage{
			To:       identifier.Value,
			Subject:  subject,
			TextBody: textBody,
			HTMLBody: strings.TrimSpace(
				renderedMessage.HTMLBody,
			),
		},
	); err != nil {
		return fmt.Errorf(
			"send OTP email through provider: %w",
			err,
		)
	}

	return nil
}

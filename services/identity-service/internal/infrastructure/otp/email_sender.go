package otp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

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

type ProviderEmailSenderOption func(
	sender *ProviderEmailSender,
) error

type ProviderEmailSender struct {
	provider        EmailProvider
	renderer        EmailMessageRenderer
	providerName    DeliveryMetricProvider
	metricsRecorder DeliveryMetricsRecorder
}

func WithEmailProviderName(
	providerName DeliveryMetricProvider,
) ProviderEmailSenderOption {
	return func(
		sender *ProviderEmailSender,
	) error {
		providerName = normalizeDeliveryMetricProvider(
			providerName,
		)

		if providerName == "" {
			return errors.New(
				"email provider name is required",
			)
		}

		sender.providerName = providerName

		return nil
	}
}

func WithEmailDeliveryMetricsRecorder(
	recorder DeliveryMetricsRecorder,
) ProviderEmailSenderOption {
	return func(
		sender *ProviderEmailSender,
	) error {
		if recorder == nil {
			return errors.New(
				"email delivery metrics recorder is required",
			)
		}

		sender.metricsRecorder = recorder

		return nil
	}
}

func NewProviderEmailSender(
	provider EmailProvider,
	renderer EmailMessageRenderer,
	options ...ProviderEmailSenderOption,
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

	sender := &ProviderEmailSender{
		provider: provider,
		renderer: renderer,
	}

	for _, option := range options {
		if option == nil {
			return nil, errors.New(
				"email sender option cannot be nil",
			)
		}

		if err := option(sender); err != nil {
			return nil, fmt.Errorf(
				"configure email sender option: %w",
				err,
			)
		}
	}

	if sender.metricsRecorder != nil &&
		sender.providerName == "" {
		return nil, errors.New(
			"email provider name is required when delivery metrics are configured",
		)
	}

	return sender, nil
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

	startedAt := time.Now()

	err = s.provider.Send(
		ctx,
		EmailMessage{
			To:       identifier.Value,
			Subject:  subject,
			TextBody: textBody,
			HTMLBody: strings.TrimSpace(
				renderedMessage.HTMLBody,
			),
		},
	)

	if s.metricsRecorder != nil {
		outcome := DeliveryMetricOutcomeSuccess
		if err != nil {
			outcome = DeliveryMetricOutcomeFailed
		}

		s.metricsRecorder.RecordOTPDelivery(
			ctx,
			DeliveryMetricChannelEmail,
			s.providerName,
			outcome,
			time.Since(startedAt),
		)
	}

	if err != nil {
		return fmt.Errorf(
			"send OTP email through provider: %w",
			err,
		)
	}

	return nil
}

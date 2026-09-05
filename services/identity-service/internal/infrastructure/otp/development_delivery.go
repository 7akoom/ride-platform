package otp

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

type DevelopmentDelivery struct {
	logger          *slog.Logger
	metricsRecorder DeliveryMetricsRecorder
}

type DevelopmentDeliveryOption func(
	*DevelopmentDelivery,
) error

func WithDevelopmentDeliveryMetricsRecorder(
	recorder DeliveryMetricsRecorder,
) DevelopmentDeliveryOption {
	return func(
		delivery *DevelopmentDelivery,
	) error {
		if recorder == nil {
			return errors.New(
				"development OTP delivery metrics recorder is required",
			)
		}

		delivery.metricsRecorder = recorder

		return nil
	}
}

func NewDevelopmentDelivery(
	environment string,
	logger *slog.Logger,
	options ...DevelopmentDeliveryOption,
) (*DevelopmentDelivery, error) {
	if environment != "development" &&
		environment != "test" {
		return nil, errors.New(
			"development OTP delivery is only allowed in development or test environments",
		)
	}

	if logger == nil {
		return nil, errors.New(
			"development OTP delivery logger is required",
		)
	}

	delivery := &DevelopmentDelivery{
		logger:          logger,
		metricsRecorder: newNoopDeliveryMetricsRecorder(),
	}

	for _, option := range options {
		if option == nil {
			return nil, errors.New(
				"development OTP delivery option cannot be nil",
			)
		}

		if err := option(
			delivery,
		); err != nil {
			return nil, err
		}
	}

	return delivery, nil
}

func (d *DevelopmentDelivery) Send(
	ctx context.Context,
	input auth.OTPDeliveryInput,
) error {
	startedAt := time.Now()

	d.logger.WarnContext(
		ctx,
		"OTP delivered through development adapter",
		"otp_identifier_type", input.Identifier.Type,
		"otp_identifier", input.Identifier.Value,
		"otp_purpose", input.Purpose,
		"otp_code", input.Code,
	)

	channel, ok := developmentDeliveryMetricChannel(
		input.Channel,
	)
	if !ok {
		return nil
	}

	d.metricsRecorder.RecordOTPDelivery(
		ctx,
		channel,
		DeliveryMetricProviderDevelopment,
		DeliveryMetricOutcomeSuccess,
		time.Since(startedAt),
	)

	return nil
}

func developmentDeliveryMetricChannel(
	channel auth.OTPDeliveryChannel,
) (DeliveryMetricChannel, bool) {
	switch channel {
	case auth.OTPDeliveryChannelSMS:
		return DeliveryMetricChannelSMS, true

	case auth.OTPDeliveryChannelWhatsApp:
		return DeliveryMetricChannelWhatsApp, true

	case auth.OTPDeliveryChannelEmail:
		return DeliveryMetricChannelEmail, true

	default:
		return "", false
	}
}

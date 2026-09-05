package otp

import (
	"context"
	"errors"
	"testing"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

func TestProviderEmailSenderRecordsSuccessfulDeliveryMetric(
	t *testing.T,
) {
	metricsRecorder := &testDeliveryMetricsRecorder{}
	provider := &testEmailProvider{}

	renderer := &testEmailMessageRenderer{
		result: RenderedEmailMessage{
			Subject:  "Ride verification code",
			TextBody: "Your code is 123456.",
		},
	}

	sender, err := NewProviderEmailSender(
		provider,
		renderer,
		WithEmailProviderName(
			DeliveryMetricProviderResend,
		),
		WithEmailDeliveryMetricsRecorder(
			metricsRecorder,
		),
	)
	if err != nil {
		t.Fatalf(
			"NewProviderEmailSender() returned an error: %v",
			err,
		)
	}

	err = sender.Send(
		context.Background(),
		"user@example.com",
		"123456",
		auth.OTPPurposeLogin,
		"en",
	)
	if err != nil {
		t.Fatalf(
			"Send() returned an error: %v",
			err,
		)
	}

	requireSingleDeliveryMetric(
		t,
		metricsRecorder,
		DeliveryMetricChannelEmail,
		DeliveryMetricProviderResend,
		DeliveryMetricOutcomeSuccess,
	)
}

func TestProviderEmailSenderRecordsFailedDeliveryMetric(
	t *testing.T,
) {
	metricsRecorder := &testDeliveryMetricsRecorder{}
	providerErr := errors.New(
		"email provider unavailable",
	)

	provider := &testEmailProvider{
		err: providerErr,
	}

	renderer := &testEmailMessageRenderer{
		result: RenderedEmailMessage{
			Subject:  "Ride verification code",
			TextBody: "Your code is 123456.",
		},
	}

	sender, err := NewProviderEmailSender(
		provider,
		renderer,
		WithEmailProviderName(
			DeliveryMetricProviderResend,
		),
		WithEmailDeliveryMetricsRecorder(
			metricsRecorder,
		),
	)
	if err != nil {
		t.Fatalf(
			"NewProviderEmailSender() returned an error: %v",
			err,
		)
	}

	err = sender.Send(
		context.Background(),
		"user@example.com",
		"123456",
		auth.OTPPurposeLogin,
		"en",
	)
	if !errors.Is(
		err,
		providerErr,
	) {
		t.Fatalf(
			"Send() error = %v, expected wrapped %v",
			err,
			providerErr,
		)
	}

	requireSingleDeliveryMetric(
		t,
		metricsRecorder,
		DeliveryMetricChannelEmail,
		DeliveryMetricProviderResend,
		DeliveryMetricOutcomeFailed,
	)
}

func TestProviderEmailSenderDoesNotRecordMetricForValidationFailure(
	t *testing.T,
) {
	metricsRecorder := &testDeliveryMetricsRecorder{}
	provider := &testEmailProvider{}
	renderer := &testEmailMessageRenderer{}

	sender, err := NewProviderEmailSender(
		provider,
		renderer,
		WithEmailProviderName(
			DeliveryMetricProviderResend,
		),
		WithEmailDeliveryMetricsRecorder(
			metricsRecorder,
		),
	)
	if err != nil {
		t.Fatalf(
			"NewProviderEmailSender() returned an error: %v",
			err,
		)
	}

	err = sender.Send(
		context.Background(),
		"not-an-email",
		"123456",
		auth.OTPPurposeLogin,
		"en",
	)
	if err == nil {
		t.Fatal(
			"Send() accepted invalid email address",
		)
	}

	if provider.calls != 0 {
		t.Fatalf(
			"provider calls = %d, expected 0",
			provider.calls,
		)
	}

	if len(metricsRecorder.deliveries) != 0 {
		t.Fatalf(
			"delivery metric count = %d, expected 0",
			len(metricsRecorder.deliveries),
		)
	}
}

func TestProviderEmailSenderDoesNotRecordMetricForRendererFailure(
	t *testing.T,
) {
	metricsRecorder := &testDeliveryMetricsRecorder{}
	provider := &testEmailProvider{}
	rendererErr := errors.New(
		"render failed",
	)

	renderer := &testEmailMessageRenderer{
		err: rendererErr,
	}

	sender, err := NewProviderEmailSender(
		provider,
		renderer,
		WithEmailProviderName(
			DeliveryMetricProviderResend,
		),
		WithEmailDeliveryMetricsRecorder(
			metricsRecorder,
		),
	)
	if err != nil {
		t.Fatalf(
			"NewProviderEmailSender() returned an error: %v",
			err,
		)
	}

	err = sender.Send(
		context.Background(),
		"user@example.com",
		"123456",
		auth.OTPPurposeLogin,
		"en",
	)
	if !errors.Is(
		err,
		rendererErr,
	) {
		t.Fatalf(
			"Send() error = %v, expected wrapped %v",
			err,
			rendererErr,
		)
	}

	if provider.calls != 0 {
		t.Fatalf(
			"provider calls = %d, expected 0",
			provider.calls,
		)
	}

	if len(metricsRecorder.deliveries) != 0 {
		t.Fatalf(
			"delivery metric count = %d, expected 0",
			len(metricsRecorder.deliveries),
		)
	}
}

func TestProviderEmailSenderRequiresProviderNameWhenMetricsAreEnabled(
	t *testing.T,
) {
	sender, err := NewProviderEmailSender(
		&testEmailProvider{},
		&testEmailMessageRenderer{},
		WithEmailDeliveryMetricsRecorder(
			&testDeliveryMetricsRecorder{},
		),
	)

	if err == nil {
		t.Fatal(
			"NewProviderEmailSender() accepted metrics without provider name",
		)
	}

	if sender != nil {
		t.Fatal(
			"NewProviderEmailSender() returned sender with invalid metric metadata",
		)
	}
}

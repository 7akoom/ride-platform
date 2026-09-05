package otp

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

func TestNewDevelopmentDeliveryAllowsDevelopment(t *testing.T) {
	logger := slog.New(
		slog.NewTextHandler(io.Discard, nil),
	)

	delivery, err := NewDevelopmentDelivery(
		"development",
		logger,
	)
	if err != nil {
		t.Fatalf(
			"NewDevelopmentDelivery() rejected development environment: %v",
			err,
		)
	}

	if delivery == nil {
		t.Fatal("NewDevelopmentDelivery() returned nil delivery")
	}
}

func TestNewDevelopmentDeliveryRejectsProduction(t *testing.T) {
	logger := slog.New(
		slog.NewTextHandler(io.Discard, nil),
	)

	delivery, err := NewDevelopmentDelivery(
		"production",
		logger,
	)

	if err == nil {
		t.Fatal(
			"NewDevelopmentDelivery() accepted production environment",
		)
	}

	if delivery != nil {
		t.Fatal(
			"NewDevelopmentDelivery() returned delivery for production environment",
		)
	}
}

func TestNewDevelopmentDeliveryRejectsNilLogger(
	t *testing.T,
) {
	delivery, err := NewDevelopmentDelivery(
		"development",
		nil,
	)

	if err == nil {
		t.Fatal(
			"NewDevelopmentDelivery() accepted nil logger",
		)
	}

	if delivery != nil {
		t.Fatal(
			"NewDevelopmentDelivery() returned delivery for nil logger",
		)
	}
}

type recordingDevelopmentDeliveryMetrics struct {
	calls []developmentDeliveryMetricCall
}

type developmentDeliveryMetricCall struct {
	channel  DeliveryMetricChannel
	provider DeliveryMetricProvider
	outcome  DeliveryMetricOutcome
	duration time.Duration
}

func (r *recordingDevelopmentDeliveryMetrics) RecordOTPDelivery(
	_ context.Context,
	channel DeliveryMetricChannel,
	provider DeliveryMetricProvider,
	outcome DeliveryMetricOutcome,
	duration time.Duration,
) {
	r.calls = append(
		r.calls,
		developmentDeliveryMetricCall{
			channel:  channel,
			provider: provider,
			outcome:  outcome,
			duration: duration,
		},
	)
}

func TestDevelopmentDeliveryRecordsEmailMetric(
	t *testing.T,
) {
	logger := slog.New(
		slog.NewTextHandler(io.Discard, nil),
	)

	recorder := &recordingDevelopmentDeliveryMetrics{}

	delivery, err := NewDevelopmentDelivery(
		"development",
		logger,
		WithDevelopmentDeliveryMetricsRecorder(
			recorder,
		),
	)
	if err != nil {
		t.Fatalf(
			"NewDevelopmentDelivery() returned an error: %v",
			err,
		)
	}

	err = delivery.Send(
		context.Background(),
		auth.OTPDeliveryInput{
			Identifier: auth.Identifier{
				Type:  auth.IdentifierTypeEmail,
				Value: "test@example.com",
			},
			Code:    "123456",
			Purpose: auth.OTPPurposeLogin,
			Channel: auth.OTPDeliveryChannelEmail,
		},
	)
	if err != nil {
		t.Fatalf(
			"Send() returned an error: %v",
			err,
		)
	}

	if len(recorder.calls) != 1 {
		t.Fatalf(
			"metric call count = %d, expected 1",
			len(recorder.calls),
		)
	}

	call := recorder.calls[0]

	if call.channel != DeliveryMetricChannelEmail {
		t.Fatalf(
			"channel = %q, expected %q",
			call.channel,
			DeliveryMetricChannelEmail,
		)
	}

	if call.provider != DeliveryMetricProviderDevelopment {
		t.Fatalf(
			"provider = %q, expected %q",
			call.provider,
			DeliveryMetricProviderDevelopment,
		)
	}

	if call.outcome != DeliveryMetricOutcomeSuccess {
		t.Fatalf(
			"outcome = %q, expected %q",
			call.outcome,
			DeliveryMetricOutcomeSuccess,
		)
	}

	if call.duration < 0 {
		t.Fatalf(
			"duration = %v, expected non-negative duration",
			call.duration,
		)
	}
}

func TestDevelopmentDeliveryDoesNotRecordAutoChannel(
	t *testing.T,
) {
	logger := slog.New(
		slog.NewTextHandler(io.Discard, nil),
	)

	recorder := &recordingDevelopmentDeliveryMetrics{}

	delivery, err := NewDevelopmentDelivery(
		"development",
		logger,
		WithDevelopmentDeliveryMetricsRecorder(
			recorder,
		),
	)
	if err != nil {
		t.Fatalf(
			"NewDevelopmentDelivery() returned an error: %v",
			err,
		)
	}

	err = delivery.Send(
		context.Background(),
		auth.OTPDeliveryInput{
			Identifier: auth.Identifier{
				Type:  auth.IdentifierTypeEmail,
				Value: "test@example.com",
			},
			Code:    "123456",
			Purpose: auth.OTPPurposeLogin,
			Channel: auth.OTPDeliveryChannelAuto,
		},
	)
	if err != nil {
		t.Fatalf(
			"Send() returned an error: %v",
			err,
		)
	}

	if len(recorder.calls) != 0 {
		t.Fatalf(
			"metric call count = %d, expected 0",
			len(recorder.calls),
		)
	}
}

func TestNewDevelopmentDeliveryRejectsNilMetricsRecorder(
	t *testing.T,
) {
	logger := slog.New(
		slog.NewTextHandler(io.Discard, nil),
	)

	delivery, err := NewDevelopmentDelivery(
		"development",
		logger,
		WithDevelopmentDeliveryMetricsRecorder(
			nil,
		),
	)

	if err == nil {
		t.Fatal(
			"NewDevelopmentDelivery() accepted nil metrics recorder",
		)
	}

	if delivery != nil {
		t.Fatal(
			"NewDevelopmentDelivery() returned delivery with nil metrics recorder",
		)
	}
}

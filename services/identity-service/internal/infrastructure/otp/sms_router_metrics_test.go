package otp

import (
	"context"
	"errors"
	"testing"
	"time"
)

type testDeliveryMetric struct {
	channel  DeliveryMetricChannel
	provider DeliveryMetricProvider
	outcome  DeliveryMetricOutcome
	duration time.Duration
}

type testDeliveryMetricsRecorder struct {
	deliveries []testDeliveryMetric
}

func (r *testDeliveryMetricsRecorder) RecordOTPDelivery(
	_ context.Context,
	channel DeliveryMetricChannel,
	provider DeliveryMetricProvider,
	outcome DeliveryMetricOutcome,
	duration time.Duration,
) {
	r.deliveries = append(
		r.deliveries,
		testDeliveryMetric{
			channel:  channel,
			provider: provider,
			outcome:  outcome,
			duration: duration,
		},
	)
}

func TestSMSRouterRecordsSuccessfulRouteDeliveryMetric(
	t *testing.T,
) {
	metricsRecorder := &testDeliveryMetricsRecorder{}
	iraqProvider := &testSMSProvider{}

	router, err := NewSMSRouter(
		[]SMSRoute{
			{
				PhonePrefix:  "+964",
				ProviderName: "bulksmsiraq",
				Provider:     iraqProvider,
			},
		},
		"",
		nil,
		WithSMSDeliveryMetricsRecorder(
			metricsRecorder,
		),
	)
	if err != nil {
		t.Fatalf(
			"NewSMSRouter() returned an error: %v",
			err,
		)
	}

	err = router.Send(
		context.Background(),
		SMSMessage{
			To:   "+9647501234567",
			Body: "Your verification code is 123456",
		},
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
		DeliveryMetricChannelSMS,
		DeliveryMetricProviderBulkSMSIraq,
		DeliveryMetricOutcomeSuccess,
	)
}

func TestSMSRouterRecordsSuccessfulDefaultDeliveryMetric(
	t *testing.T,
) {
	metricsRecorder := &testDeliveryMetricsRecorder{}
	defaultProvider := &testSMSProvider{}

	router, err := NewSMSRouter(
		nil,
		"telnyx",
		defaultProvider,
		WithSMSDeliveryMetricsRecorder(
			metricsRecorder,
		),
	)
	if err != nil {
		t.Fatalf(
			"NewSMSRouter() returned an error: %v",
			err,
		)
	}

	err = router.Send(
		context.Background(),
		SMSMessage{
			To:   "+971501234567",
			Body: "Your verification code is 123456",
		},
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
		DeliveryMetricChannelSMS,
		DeliveryMetricProviderTelnyx,
		DeliveryMetricOutcomeSuccess,
	)
}

func TestSMSRouterRecordsFailedProviderDeliveryMetric(
	t *testing.T,
) {
	metricsRecorder := &testDeliveryMetricsRecorder{}
	providerErr := errors.New(
		"provider unavailable",
	)

	iraqProvider := &testSMSProvider{
		err: providerErr,
	}

	router, err := NewSMSRouter(
		[]SMSRoute{
			{
				PhonePrefix:  "+964",
				ProviderName: "bulksmsiraq",
				Provider:     iraqProvider,
			},
		},
		"",
		nil,
		WithSMSDeliveryMetricsRecorder(
			metricsRecorder,
		),
	)
	if err != nil {
		t.Fatalf(
			"NewSMSRouter() returned an error: %v",
			err,
		)
	}

	err = router.Send(
		context.Background(),
		SMSMessage{
			To:   "+9647501234567",
			Body: "Your verification code is 123456",
		},
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
		DeliveryMetricChannelSMS,
		DeliveryMetricProviderBulkSMSIraq,
		DeliveryMetricOutcomeFailed,
	)
}

func TestSMSRouterDoesNotRecordDeliveryMetricWithoutProviderAttempt(
	t *testing.T,
) {
	metricsRecorder := &testDeliveryMetricsRecorder{}

	router, err := NewSMSRouter(
		[]SMSRoute{
			{
				PhonePrefix:  "+964",
				ProviderName: "bulksmsiraq",
				Provider:     &testSMSProvider{},
			},
		},
		"",
		nil,
		WithSMSDeliveryMetricsRecorder(
			metricsRecorder,
		),
	)
	if err != nil {
		t.Fatalf(
			"NewSMSRouter() returned an error: %v",
			err,
		)
	}

	err = router.Send(
		context.Background(),
		SMSMessage{
			To:   "+971501234567",
			Body: "Your verification code is 123456",
		},
	)
	if err == nil {
		t.Fatal(
			"Send() accepted destination without provider",
		)
	}

	if len(metricsRecorder.deliveries) != 0 {
		t.Fatalf(
			"delivery metric count = %d, expected 0",
			len(metricsRecorder.deliveries),
		)
	}
}

func requireSingleDeliveryMetric(
	t *testing.T,
	recorder *testDeliveryMetricsRecorder,
	channel DeliveryMetricChannel,
	provider DeliveryMetricProvider,
	outcome DeliveryMetricOutcome,
) {
	t.Helper()

	if len(recorder.deliveries) != 1 {
		t.Fatalf(
			"delivery metric count = %d, expected 1",
			len(recorder.deliveries),
		)
	}

	metric := recorder.deliveries[0]

	if metric.channel != channel {
		t.Fatalf(
			"delivery metric channel = %q, expected %q",
			metric.channel,
			channel,
		)
	}

	if metric.provider != provider {
		t.Fatalf(
			"delivery metric provider = %q, expected %q",
			metric.provider,
			provider,
		)
	}

	if metric.outcome != outcome {
		t.Fatalf(
			"delivery metric outcome = %q, expected %q",
			metric.outcome,
			outcome,
		)
	}

	if metric.duration < 0 {
		t.Fatalf(
			"delivery metric duration = %v, expected non-negative duration",
			metric.duration,
		)
	}
}

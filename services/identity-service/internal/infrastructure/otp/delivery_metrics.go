package otp

import (
	"context"
	"time"
)

type DeliveryMetricChannel string

const (
	DeliveryMetricChannelSMS      DeliveryMetricChannel = "sms"
	DeliveryMetricChannelWhatsApp DeliveryMetricChannel = "whatsapp"
	DeliveryMetricChannelEmail    DeliveryMetricChannel = "email"
)

type DeliveryMetricProvider string

const (
	DeliveryMetricProviderTelnyx      DeliveryMetricProvider = "telnyx"
	DeliveryMetricProviderMeta        DeliveryMetricProvider = "meta"
	DeliveryMetricProviderBulkSMSIraq DeliveryMetricProvider = "bulksmsiraq"
	DeliveryMetricProviderResend      DeliveryMetricProvider = "resend"
	DeliveryMetricProviderDevelopment DeliveryMetricProvider = "development"
)

type DeliveryMetricOutcome string

const (
	DeliveryMetricOutcomeSuccess DeliveryMetricOutcome = "success"
	DeliveryMetricOutcomeFailed  DeliveryMetricOutcome = "failed"
)

type ProviderHealthMetricEvent string

const (
	ProviderHealthMetricEventCircuitOpen ProviderHealthMetricEvent = "circuit_open"
)

type DeliveryMetricsRecorder interface {
	RecordOTPDelivery(
		ctx context.Context,
		channel DeliveryMetricChannel,
		provider DeliveryMetricProvider,
		outcome DeliveryMetricOutcome,
		duration time.Duration,
	)
}

type ProviderHealthMetricsRecorder interface {
	RecordOTPProviderHealthEvent(
		ctx context.Context,
		channel DeliveryMetricChannel,
		provider DeliveryMetricProvider,
		event ProviderHealthMetricEvent,
	)
}

type noopDeliveryMetricsRecorder struct{}

func (noopDeliveryMetricsRecorder) RecordOTPDelivery(
	context.Context,
	DeliveryMetricChannel,
	DeliveryMetricProvider,
	DeliveryMetricOutcome,
	time.Duration,
) {
}

func newNoopDeliveryMetricsRecorder() DeliveryMetricsRecorder {
	return noopDeliveryMetricsRecorder{}
}

type noopProviderHealthMetricsRecorder struct{}

func (noopProviderHealthMetricsRecorder) RecordOTPProviderHealthEvent(
	context.Context,
	DeliveryMetricChannel,
	DeliveryMetricProvider,
	ProviderHealthMetricEvent,
) {
}

func newNoopProviderHealthMetricsRecorder() ProviderHealthMetricsRecorder {
	return noopProviderHealthMetricsRecorder{}
}

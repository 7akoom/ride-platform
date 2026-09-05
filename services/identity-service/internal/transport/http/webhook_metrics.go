package httptransport

import (
	"context"
	"errors"

	"github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/otp"
)

type DeliveryWebhookOutcome string

const (
	DeliveryWebhookAccepted          DeliveryWebhookOutcome = "accepted"
	DeliveryWebhookIgnored           DeliveryWebhookOutcome = "ignored"
	DeliveryWebhookUnauthorized      DeliveryWebhookOutcome = "unauthorized"
	DeliveryWebhookInvalid           DeliveryWebhookOutcome = "invalid"
	DeliveryWebhookPersistenceFailed DeliveryWebhookOutcome = "persistence_failed"
)

type DeliveryWebhookMetricsRecorder interface {
	RecordDeliveryWebhook(context.Context, otp.DeliveryTrackingProvider, DeliveryWebhookOutcome)
}

type DeliveryReceiptHandlerOption func(*DeliveryReceiptHandler) error

func WithDeliveryWebhookMetrics(provider otp.DeliveryTrackingProvider, recorder DeliveryWebhookMetricsRecorder) DeliveryReceiptHandlerOption {
	return func(h *DeliveryReceiptHandler) error {
		if provider != otp.DeliveryTrackingProviderTelnyx && provider != otp.DeliveryTrackingProviderMeta {
			return errors.New("unsupported delivery webhook metric provider")
		}
		if recorder == nil {
			return errors.New("delivery webhook metrics recorder is required")
		}
		h.provider = provider
		h.metrics = recorder
		return nil
	}
}

func (h *DeliveryReceiptHandler) configure(options []DeliveryReceiptHandlerOption) (*DeliveryReceiptHandler, error) {
	for _, option := range options {
		if option == nil {
			return nil, errors.New("delivery receipt handler option is required")
		}
		if err := option(h); err != nil {
			return nil, err
		}
	}
	return h, nil
}

func (h *DeliveryReceiptHandler) recordOutcome(ctx context.Context, outcome DeliveryWebhookOutcome) {
	if h.metrics != nil {
		h.metrics.RecordDeliveryWebhook(ctx, h.provider, outcome)
	}
}

func deliveryWebhookDecodeOutcome(err error) DeliveryWebhookOutcome {
	switch {
	case errors.Is(err, otp.ErrDeliveryWebhookIgnored):
		return DeliveryWebhookIgnored
	case errors.Is(err, otp.ErrDeliveryWebhookUnauthorized):
		return DeliveryWebhookUnauthorized
	default:
		return DeliveryWebhookInvalid
	}
}

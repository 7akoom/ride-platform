package observability

import (
	"context"

	"github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/otp"
	httptransport "github.com/7akoom/ride-platform/services/identity-service/internal/transport/http"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var _ httptransport.DeliveryWebhookMetricsRecorder = (*AuthMetricsRecorder)(nil)

func (r *AuthMetricsRecorder) RecordDeliveryWebhook(ctx context.Context, provider otp.DeliveryTrackingProvider, outcome httptransport.DeliveryWebhookOutcome) {
	if r == nil || r.metrics == nil {
		return
	}
	if provider != otp.DeliveryTrackingProviderTelnyx && provider != otp.DeliveryTrackingProviderMeta {
		return
	}
	switch outcome {
	case httptransport.DeliveryWebhookAccepted, httptransport.DeliveryWebhookIgnored,
		httptransport.DeliveryWebhookUnauthorized, httptransport.DeliveryWebhookInvalid,
		httptransport.DeliveryWebhookPersistenceFailed:
	default:
		return
	}
	r.metrics.deliveryWebhooks.Add(ctx, 1, metric.WithAttributes(
		attribute.String("provider", string(provider)),
		attribute.String("outcome", string(outcome)),
	))
}

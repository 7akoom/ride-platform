package observability

import (
	"context"
	"testing"

	"github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/otp"
	httptransport "github.com/7akoom/ride-platform/services/identity-service/internal/transport/http"
)

func TestDeliveryWebhookMetricLabels(t *testing.T) {
	for _, provider := range []otp.DeliveryTrackingProvider{otp.DeliveryTrackingProviderTelnyx, otp.DeliveryTrackingProviderMeta} {
		for _, outcome := range []httptransport.DeliveryWebhookOutcome{
			httptransport.DeliveryWebhookAccepted, httptransport.DeliveryWebhookIgnored,
			httptransport.DeliveryWebhookUnauthorized, httptransport.DeliveryWebhookInvalid,
			httptransport.DeliveryWebhookPersistenceFailed,
		} {
			t.Run(string(provider)+"/"+string(outcome), func(t *testing.T) {
				metrics, reader := newTestAuthMetrics(t)
				recorder, err := NewAuthMetricsRecorder(metrics)
				if err != nil {
					t.Fatal(err)
				}
				recorder.RecordDeliveryWebhook(context.Background(), provider, outcome)
				recorder.RecordDeliveryWebhook(context.Background(), "untrusted", outcome)
				recorder.RecordDeliveryWebhook(context.Background(), provider, "untrusted")
				requireInt64SumMetric(t, collectAuthMetrics(t, reader), "identity.otp.delivery.webhooks",
					map[string]string{"provider": string(provider), "outcome": string(outcome)}, 1)
			})
		}
	}
}

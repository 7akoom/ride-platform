package observability

import (
	"context"
	"errors"
	"testing"

	"github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/otp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
)

func TestProviderHealthMetricRecorder(t *testing.T) {
	metrics, reader := newTestAuthMetrics(t)
	recorder, err := NewAuthMetricsRecorder(metrics)
	if err != nil {
		t.Fatal(err)
	}
	recorder.RecordOTPProviderHealthEvent(context.Background(), otp.DeliveryMetricChannelWhatsApp,
		otp.DeliveryMetricProviderBulkSMSIraq, otp.ProviderHealthMetricEventCircuitOpen)
	requireInt64SumMetric(t, collectAuthMetrics(t, reader), "identity.otp.provider_health.events",
		map[string]string{"channel": "whatsapp", "provider": "bulksmsiraq", "event": "circuit_open"}, 1)
}

type failingDeliveryHistogramMeter struct {
	metric.Meter
	err error
}

func (m failingDeliveryHistogramMeter) Float64Histogram(name string, options ...metric.Float64HistogramOption) (metric.Float64Histogram, error) {
	if name == "identity.otp.delivery.duration" {
		return nil, m.err
	}
	return m.Meter.Float64Histogram(name, options...)
}

func TestAuthMetricsPropagatesDeliveryHistogramInitializationFailure(t *testing.T) {
	want := errors.New("histogram initialization failed")
	metrics, err := NewAuthMetricsWithMeter(failingDeliveryHistogramMeter{
		Meter: noop.NewMeterProvider().Meter("test"), err: want,
	})
	if metrics != nil || !errors.Is(err, want) {
		t.Fatal("expected histogram initialization error and no metrics")
	}
}

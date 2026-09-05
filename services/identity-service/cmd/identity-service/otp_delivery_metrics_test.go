package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	"github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/otp"
)

type runtimeDeliveryRecorder struct {
	deliveries []otp.DeliveryMetricProvider
}

func (r *runtimeDeliveryRecorder) RecordOTPDelivery(_ context.Context, _ otp.DeliveryMetricChannel, provider otp.DeliveryMetricProvider, _ otp.DeliveryMetricOutcome, _ time.Duration) {
	r.deliveries = append(r.deliveries, provider)
}

type runtimeHealthRecorder struct {
	runtimeDeliveryRecorder
	calls    int
	channel  otp.DeliveryMetricChannel
	provider otp.DeliveryMetricProvider
	event    otp.ProviderHealthMetricEvent
}

func (r *runtimeHealthRecorder) RecordOTPProviderHealthEvent(_ context.Context, channel otp.DeliveryMetricChannel, provider otp.DeliveryMetricProvider, event otp.ProviderHealthMetricEvent) {
	r.calls++
	r.channel, r.provider, r.event = channel, provider, event
}

type runtimeProviderClient struct {
	primaryCalls  int
	fallbackCalls int
}

func (c *runtimeProviderClient) Do(request *http.Request) (*http.Response, error) {
	status, body := http.StatusOK, `{"messages":[{"id":"test-message"}]}`
	if request.URL.Host == "sms.example.com" {
		c.primaryCalls++
		status, body = http.StatusInternalServerError, `{}`
	} else {
		c.fallbackCalls++
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
}

func TestWhatsAppRuntimeWiresOptionalProviderHealthMetrics(t *testing.T) {
	for _, healthEnabled := range []bool{false, true} {
		name := "delivery_only"
		if healthEnabled {
			name = "delivery_and_health"
		}
		t.Run(name, func(t *testing.T) {
			cfg := baseProductionOTPConfig()
			cfg.WhatsAppDefaultProvider = "bulksmsiraq"
			cfg.WhatsAppFallbackProvider = "meta"
			cfg.WhatsAppProviderHealthFailureThreshold = "1"
			cfg.WhatsAppProviderHealthCooldown = "1m"
			cfg.BulkSMSIraqOTPEndpoint = "https://sms.example.com/api/otp/send"
			health := &runtimeHealthRecorder{}
			var recorder otp.DeliveryMetricsRecorder = &health.runtimeDeliveryRecorder
			if healthEnabled {
				recorder = health
			}
			client := &runtimeProviderClient{}
			sender, err := buildWhatsAppSenderWithTracking(client, cfg, nil, recorder)
			if err != nil {
				t.Fatal(err)
			}
			send := func() error {
				return sender.Send(context.Background(), "+9647501234567", "123456", auth.OTPPurposeLogin, "en")
			}
			if send() == nil {
				t.Fatal("expected primary failure without immediate fallback")
			}
			if err := send(); err != nil {
				t.Fatal("expected fallback success after circuit opened")
			}
			if client.primaryCalls != 1 || client.fallbackCalls != 1 {
				t.Fatal("unexpected provider call counts")
			}
			if len(health.deliveries) != 2 || health.deliveries[0] != otp.DeliveryMetricProviderBulkSMSIraq || health.deliveries[1] != otp.DeliveryMetricProviderMeta {
				t.Fatal("delivery metrics must count only actual provider attempts")
			}
			if healthEnabled && (health.calls != 1 || health.channel != otp.DeliveryMetricChannelWhatsApp || health.provider != otp.DeliveryMetricProviderBulkSMSIraq || health.event != otp.ProviderHealthMetricEventCircuitOpen) {
				t.Fatal("expected exactly one whatsapp/bulksmsiraq/circuit_open metric")
			}
			if !healthEnabled && health.calls != 0 {
				t.Fatal("unexpected health metric for delivery-only recorder")
			}
		})
	}
}

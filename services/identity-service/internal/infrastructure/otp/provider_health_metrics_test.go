package otp

import (
	"context"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

type recordedProviderHealthEvent struct {
	channel  DeliveryMetricChannel
	provider DeliveryMetricProvider
	event    ProviderHealthMetricEvent
}

type providerHealthEventRecorder struct{ events []recordedProviderHealthEvent }

func (r *providerHealthEventRecorder) RecordOTPProviderHealthEvent(_ context.Context, channel DeliveryMetricChannel, provider DeliveryMetricProvider, event ProviderHealthMetricEvent) {
	r.events = append(r.events, recordedProviderHealthEvent{channel, provider, event})
}

func TestRoutersRecordOnlyReachedCircuitSkips(t *testing.T) {
	for _, channel := range []DeliveryMetricChannel{DeliveryMetricChannelSMS, DeliveryMetricChannelWhatsApp} {
		for _, tc := range []struct {
			name                      string
			primaryOpen, primaryFails bool
			skips, deliveries         int
		}{
			{"unused_open_fallback", false, false, 0, 1},
			{"open_fallback_after_rate_limit", false, true, 1, 1},
			{"both_open", true, false, 2, 0},
		} {
			t.Run(string(channel)+"/"+tc.name, func(t *testing.T) {
				tracker, err := NewCircuitBreakerProviderHealthTracker(1, time.Minute)
				if err != nil {
					t.Fatal(err)
				}
				fallbackName := DeliveryMetricProviderTelnyx
				if channel == DeliveryMetricChannelWhatsApp {
					fallbackName = DeliveryMetricProviderMeta
				}
				tracker.RecordFailure(DeliveryTrackingChannel(channel), DeliveryTrackingProvider(fallbackName), time.Now())
				if tc.primaryOpen {
					tracker.RecordFailure(DeliveryTrackingChannel(channel), DeliveryTrackingProviderBulkSMSIraq, time.Now())
				}
				var primaryErr error
				if tc.primaryFails {
					primaryErr = &SMSProviderError{Provider: "bulksmsiraq", Kind: SMSProviderErrorRateLimited}
				}
				health := &providerHealthEventRecorder{}
				delivery := &testDeliveryMetricsRecorder{}
				var sendErr error
				var primaryCalls, fallbackCalls int
				if channel == DeliveryMetricChannelSMS {
					primary, fallback := &testTrackedSMSProvider{err: primaryErr}, &testTrackedSMSProvider{}
					router, err := NewSMSRouter(nil, "bulksmsiraq", primary,
						WithSMSFallbackProvider(string(fallbackName), fallback, ConservativeProviderFailoverPolicy{}),
						WithSMSProviderHealthTracker(tracker), WithSMSProviderHealthMetricsRecorder(health), WithSMSDeliveryMetricsRecorder(delivery))
					if err != nil {
						t.Fatal(err)
					}
					sendErr = router.Send(context.Background(), SMSMessage{To: "+9647501234567", Body: "test message"})
					primaryCalls, fallbackCalls = primary.sendCalls, fallback.sendCalls
				} else {
					primary, fallback := &testWhatsAppProvider{err: primaryErr}, &testWhatsAppProvider{}
					router, err := NewWhatsAppRouter(nil, primary, WithWhatsAppRouterDefaultProviderName(DeliveryMetricProviderBulkSMSIraq),
						WithWhatsAppFallbackProvider(fallbackName, fallback, ConservativeProviderFailoverPolicy{}),
						WithWhatsAppProviderHealthTracker(tracker), WithWhatsAppProviderHealthMetricsRecorder(health), WithWhatsAppRouterDeliveryMetricsRecorder(delivery))
					if err != nil {
						t.Fatal(err)
					}
					sendErr = router.SendOTP(context.Background(), WhatsAppOTPProviderInput{PhoneNumber: "+9647501234567", Code: "123456", Purpose: auth.OTPPurposeLogin, Locale: "en"})
					primaryCalls, fallbackCalls = primary.calls, fallback.calls
				}
				if (sendErr != nil) != (tc.skips > 0) {
					t.Fatal("failover result changed")
				}
				if primaryCalls != tc.deliveries || fallbackCalls != 0 || len(delivery.deliveries) != tc.deliveries {
					t.Fatal("skipped providers must not be called or counted as attempts")
				}
				if len(health.events) != tc.skips {
					t.Fatal("incorrect circuit skip count")
				}
				for i, event := range health.events {
					wantProvider := fallbackName
					if tc.primaryOpen && i == 0 {
						wantProvider = DeliveryMetricProviderBulkSMSIraq
					}
					if event.channel != channel || event.provider != wantProvider || event.event != ProviderHealthMetricEventCircuitOpen {
						t.Fatal("incorrect circuit skip labels")
					}
				}
			})
		}
	}
}

package observability

import (
	"context"
	"testing"
	"time"

	authapp "github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	otpinfra "github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/otp"
	tokeninfra "github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/token"
)

func TestAuthMetricsRecorderMapsApplicationAndDeliveryMetrics(
	t *testing.T,
) {
	metrics, reader := newTestAuthMetrics(t)

	recorder, err := NewAuthMetricsRecorder(
		metrics,
	)
	if err != nil {
		t.Fatalf(
			"NewAuthMetricsRecorder() returned an error: %v",
			err,
		)
	}

	ctx := context.Background()

	recorder.RecordOTPRequest(
		ctx,
		authapp.OTPPurposeLinkIdentifier,
		authapp.OTPDeliveryChannelSMS,
		authapp.MetricOutcomeSuccess,
	)

	recorder.RecordOTPVerification(
		ctx,
		authapp.OTPPurposeLinkIdentifier,
		authapp.MetricOutcomeSuccess,
	)

	recorder.RecordSessionOperation(
		ctx,
		authapp.SessionMetricOperationCreate,
		authapp.MetricOutcomeSuccess,
	)

	recorder.RecordSecurityEvent(
		ctx,
		authapp.SecurityMetricEventRefreshTokenReuse,
	)

	recorder.RecordOTPDelivery(
		ctx,
		otpinfra.DeliveryMetricChannelSMS,
		otpinfra.DeliveryMetricProviderBulkSMSIraq,
		otpinfra.DeliveryMetricOutcomeSuccess,
		2*time.Second,
	)

	resourceMetrics := collectAuthMetrics(
		t,
		reader,
	)

	requireInt64SumMetric(
		t,
		resourceMetrics,
		"identity.otp.requests",
		map[string]string{
			"purpose": "link_identifier",
			"channel": "sms",
			"outcome": "success",
		},
		1,
	)

	requireInt64SumMetric(
		t,
		resourceMetrics,
		"identity.otp.verifications",
		map[string]string{
			"purpose": "link_identifier",
			"outcome": "success",
		},
		1,
	)

	requireInt64SumMetric(
		t,
		resourceMetrics,
		"identity.sessions.operations",
		map[string]string{
			"operation": "create",
			"outcome":   "success",
		},
		1,
	)

	requireInt64SumMetric(
		t,
		resourceMetrics,
		"identity.security.events",
		map[string]string{
			"event": "refresh_token_reuse",
		},
		1,
	)

	requireInt64SumMetric(
		t,
		resourceMetrics,
		"identity.otp.deliveries",
		map[string]string{
			"channel":  "sms",
			"provider": "bulksmsiraq",
			"outcome":  "success",
		},
		1,
	)

	requireFloat64HistogramMetric(
		t,
		resourceMetrics,
		"identity.otp.delivery.duration",
		map[string]string{
			"channel":  "sms",
			"provider": "bulksmsiraq",
			"outcome":  "success",
		},
		1,
		2,
	)
}

func TestAuthMetricsRecorderMapsAuthOperation(
	t *testing.T,
) {
	metrics, reader := newTestAuthMetrics(t)

	recorder, err := NewAuthMetricsRecorder(
		metrics,
	)
	if err != nil {
		t.Fatalf(
			"NewAuthMetricsRecorder() returned an error: %v",
			err,
		)
	}

	recorder.RecordAuthOperation(
		context.Background(),
		authapp.AuthMetricOperationRefresh,
		authapp.MetricOutcomeSuccess,
		1500*time.Millisecond,
	)

	resourceMetrics := collectAuthMetrics(
		t,
		reader,
	)

	expectedAttributes := map[string]string{
		"operation": "refresh",
		"outcome":   "success",
	}

	requireInt64SumMetric(
		t,
		resourceMetrics,
		"identity.auth.operations",
		expectedAttributes,
		1,
	)

	requireFloat64HistogramMetric(
		t,
		resourceMetrics,
		"identity.auth.duration",
		expectedAttributes,
		1,
		1.5,
	)
}

func TestAuthMetricsRecorderMapsAccessTokenVerification(
	t *testing.T,
) {
	metrics, reader := newTestAuthMetrics(t)

	recorder, err := NewAuthMetricsRecorder(
		metrics,
	)
	if err != nil {
		t.Fatalf(
			"NewAuthMetricsRecorder() returned an error: %v",
			err,
		)
	}

	recorder.RecordAccessTokenVerification(
		context.Background(),
		tokeninfra.AccessTokenVerificationMetricOutcomeSuccess,
		250*time.Millisecond,
	)

	resourceMetrics := collectAuthMetrics(
		t,
		reader,
	)

	expectedAttributes := map[string]string{
		"operation": "access_token_verify",
		"outcome":   "success",
	}

	requireInt64SumMetric(
		t,
		resourceMetrics,
		"identity.auth.operations",
		expectedAttributes,
		1,
	)

	requireFloat64HistogramMetric(
		t,
		resourceMetrics,
		"identity.auth.duration",
		expectedAttributes,
		1,
		0.25,
	)
}

func TestNewAuthMetricsRecorderRejectsNilMetrics(
	t *testing.T,
) {
	recorder, err := NewAuthMetricsRecorder(
		nil,
	)

	if err == nil {
		t.Fatal(
			"NewAuthMetricsRecorder() accepted nil metrics",
		)
	}

	if recorder != nil {
		t.Fatal(
			"NewAuthMetricsRecorder() returned recorder for nil metrics",
		)
	}
}

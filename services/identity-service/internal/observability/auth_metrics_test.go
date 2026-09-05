package observability

import (
	"context"
	"math"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestNewAuthMetricsWithMeterRejectsNilMeter(
	t *testing.T,
) {
	t.Parallel()

	metrics, err := NewAuthMetricsWithMeter(nil)
	if err == nil {
		t.Fatal("expected error for nil meter")
	}

	if metrics != nil {
		t.Fatal("expected nil metrics")
	}
}

func TestAuthMetricsRecordsExpectedMeasurements(
	t *testing.T,
) {
	t.Parallel()

	metrics, reader := newTestAuthMetrics(t)

	ctx := context.Background()

	metrics.RecordAuthOperation(
		ctx,
		AuthMetricOperationLogin,
		MetricOutcomeSuccess,
		250*time.Millisecond,
	)

	metrics.RecordOTPRequest(
		ctx,
		OTPMetricPurposeLogin,
		OTPMetricChannelSMS,
		MetricOutcomeRejected,
	)

	metrics.RecordOTPVerification(
		ctx,
		OTPMetricPurposeIdentifierLink,
		MetricOutcomeFailed,
	)

	metrics.RecordOTPDelivery(
		ctx,
		OTPMetricChannelEmail,
		OTPMetricProviderResend,
		MetricOutcomeSuccess,
		125*time.Millisecond,
	)

	metrics.RecordSessionOperation(
		ctx,
		SessionMetricOperationRevoke,
		MetricOutcomeSuccess,
	)

	metrics.RecordSecurityEvent(
		ctx,
		SecurityMetricEventRefreshTokenReuse,
	)

	resourceMetrics := collectAuthMetrics(
		t,
		reader,
	)

	requireInt64SumMetric(
		t,
		resourceMetrics,
		"identity.auth.operations",
		map[string]string{
			"operation": "login",
			"outcome":   "success",
		},
		1,
	)

	requireFloat64HistogramMetric(
		t,
		resourceMetrics,
		"identity.auth.duration",
		map[string]string{
			"operation": "login",
			"outcome":   "success",
		},
		1,
		0.25,
	)

	requireInt64SumMetric(
		t,
		resourceMetrics,
		"identity.otp.requests",
		map[string]string{
			"purpose": "login",
			"channel": "sms",
			"outcome": "rejected",
		},
		1,
	)

	requireInt64SumMetric(
		t,
		resourceMetrics,
		"identity.otp.verifications",
		map[string]string{
			"purpose": "link_identifier",
			"outcome": "failed",
		},
		1,
	)

	requireInt64SumMetric(
		t,
		resourceMetrics,
		"identity.otp.deliveries",
		map[string]string{
			"channel":  "email",
			"provider": "resend",
			"outcome":  "success",
		},
		1,
	)

	requireFloat64HistogramMetric(
		t,
		resourceMetrics,
		"identity.otp.delivery.duration",
		map[string]string{
			"channel":  "email",
			"provider": "resend",
			"outcome":  "success",
		},
		1,
		0.125,
	)

	requireInt64SumMetric(
		t,
		resourceMetrics,
		"identity.sessions.operations",
		map[string]string{
			"operation": "revoke",
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

	requireSafeMetricAttributes(
		t,
		resourceMetrics,
	)
}

func TestAuthMetricsNilReceiverIsSafe(
	t *testing.T,
) {
	t.Parallel()

	var metrics *AuthMetrics

	ctx := context.Background()

	metrics.RecordAuthOperation(
		ctx,
		AuthMetricOperationLogin,
		MetricOutcomeSuccess,
		time.Second,
	)

	metrics.RecordOTPRequest(
		ctx,
		OTPMetricPurposeLogin,
		OTPMetricChannelSMS,
		MetricOutcomeSuccess,
	)

	metrics.RecordOTPVerification(
		ctx,
		OTPMetricPurposeLogin,
		MetricOutcomeSuccess,
	)

	metrics.RecordOTPDelivery(
		ctx,
		OTPMetricChannelSMS,
		OTPMetricProviderTelnyx,
		MetricOutcomeSuccess,
		time.Second,
	)

	metrics.RecordSessionOperation(
		ctx,
		SessionMetricOperationCreate,
		MetricOutcomeSuccess,
	)

	metrics.RecordSecurityEvent(
		ctx,
		SecurityMetricEventRefreshTokenReuse,
	)
}

func newTestAuthMetrics(
	t *testing.T,
) (*AuthMetrics, *sdkmetric.ManualReader) {
	t.Helper()

	reader := sdkmetric.NewManualReader()

	provider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(reader),
	)

	t.Cleanup(func() {
		if err := provider.Shutdown(
			context.Background(),
		); err != nil {
			t.Errorf(
				"shutdown meter provider: %v",
				err,
			)
		}
	})

	metrics, err := NewAuthMetricsWithMeter(
		provider.Meter(
			authMetricsInstrumentationName,
		),
	)
	if err != nil {
		t.Fatalf(
			"create auth metrics: %v",
			err,
		)
	}

	return metrics, reader
}

func collectAuthMetrics(
	t *testing.T,
	reader *sdkmetric.ManualReader,
) metricdata.ResourceMetrics {
	t.Helper()

	var resourceMetrics metricdata.ResourceMetrics

	if err := reader.Collect(
		context.Background(),
		&resourceMetrics,
	); err != nil {
		t.Fatalf(
			"collect metrics: %v",
			err,
		)
	}

	return resourceMetrics
}

func requireInt64SumMetric(
	t *testing.T,
	resourceMetrics metricdata.ResourceMetrics,
	name string,
	expectedAttributes map[string]string,
	expectedValue int64,
) {
	t.Helper()

	metricValue := findCollectedMetric(
		t,
		resourceMetrics,
		name,
	)

	sum, ok := metricValue.Data.(metricdata.Sum[int64])

	if !ok {
		t.Fatalf(
			"metric %q aggregation = %T, expected metricdata.Sum[int64]",
			name,
			metricValue.Data,
		)
	}

	for _, point := range sum.DataPoints {
		if !metricAttributesMatch(
			point.Attributes,
			expectedAttributes,
		) {
			continue
		}

		if point.Value != expectedValue {
			t.Fatalf(
				"metric %q value = %d, expected %d",
				name,
				point.Value,
				expectedValue,
			)
		}

		return
	}

	t.Fatalf(
		"metric %q has no data point with attributes %v",
		name,
		expectedAttributes,
	)
}

func requireFloat64HistogramMetric(
	t *testing.T,
	resourceMetrics metricdata.ResourceMetrics,
	name string,
	expectedAttributes map[string]string,
	expectedCount uint64,
	expectedSum float64,
) {
	t.Helper()

	metricValue := findCollectedMetric(
		t,
		resourceMetrics,
		name,
	)

	histogram, ok := metricValue.Data.(metricdata.Histogram[float64])

	if !ok {
		t.Fatalf(
			"metric %q aggregation = %T, expected metricdata.Histogram[float64]",
			name,
			metricValue.Data,
		)
	}

	for _, point := range histogram.DataPoints {
		if !metricAttributesMatch(
			point.Attributes,
			expectedAttributes,
		) {
			continue
		}

		if point.Count != expectedCount {
			t.Fatalf(
				"metric %q count = %d, expected %d",
				name,
				point.Count,
				expectedCount,
			)
		}

		if math.Abs(
			point.Sum-expectedSum,
		) > 0.000001 {
			t.Fatalf(
				"metric %q sum = %f, expected %f",
				name,
				point.Sum,
				expectedSum,
			)
		}

		return
	}

	t.Fatalf(
		"metric %q has no data point with attributes %v",
		name,
		expectedAttributes,
	)
}

func findCollectedMetric(
	t *testing.T,
	resourceMetrics metricdata.ResourceMetrics,
	name string,
) metricdata.Metrics {
	t.Helper()

	for _, scopeMetrics := range resourceMetrics.ScopeMetrics {
		for _, metricValue := range scopeMetrics.Metrics {
			if metricValue.Name == name {
				return metricValue
			}
		}
	}

	t.Fatalf(
		"metric %q was not collected",
		name,
	)

	return metricdata.Metrics{}
}

func metricAttributesMatch(
	attributes attribute.Set,
	expected map[string]string,
) bool {
	if attributes.Len() != len(expected) {
		return false
	}

	for key, expectedValue := range expected {
		value, ok := attributes.Value(
			attribute.Key(key),
		)
		if !ok {
			return false
		}

		if value.AsString() != expectedValue {
			return false
		}
	}

	return true
}

func requireSafeMetricAttributes(
	t *testing.T,
	resourceMetrics metricdata.ResourceMetrics,
) {
	t.Helper()

	allowedKeys := map[string]struct{}{
		"operation": {},
		"outcome":   {},
		"purpose":   {},
		"channel":   {},
		"provider":  {},
		"event":     {},
	}

	checkAttributes := func(
		metricName string,
		attributes attribute.Set,
	) {
		t.Helper()

		for _, keyValue := range attributes.ToSlice() {
			key := string(keyValue.Key)

			if _, allowed := allowedKeys[key]; !allowed {
				t.Fatalf(
					"metric %q contains disallowed attribute %q",
					metricName,
					key,
				)
			}
		}
	}

	for _, scopeMetrics := range resourceMetrics.ScopeMetrics {
		for _, metricValue := range scopeMetrics.Metrics {
			switch data := metricValue.Data.(type) {
			case metricdata.Sum[int64]:
				for _, point := range data.DataPoints {
					checkAttributes(
						metricValue.Name,
						point.Attributes,
					)
				}

			case metricdata.Histogram[float64]:
				for _, point := range data.DataPoints {
					checkAttributes(
						metricValue.Name,
						point.Attributes,
					)
				}

			default:
				t.Fatalf(
					"metric %q has unsupported aggregation %T",
					metricValue.Name,
					metricValue.Data,
				)
			}
		}
	}
}

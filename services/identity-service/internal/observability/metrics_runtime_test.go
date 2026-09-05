package observability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMetricsRuntimeExposesAuthMetrics(
	t *testing.T,
) {
	runtime, err := NewMetricsRuntime(
		"identity-service",
		":9090",
	)
	if err != nil {
		t.Fatalf(
			"NewMetricsRuntime() returned an error: %v",
			err,
		)
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(
			context.Background(),
			time.Second,
		)
		defer cancel()

		if err := runtime.Shutdown(ctx); err != nil {
			t.Errorf(
				"shutdown metrics runtime: %v",
				err,
			)
		}
	})

	metrics, err := runtime.NewAuthMetrics()
	if err != nil {
		t.Fatalf(
			"NewAuthMetrics() returned an error: %v",
			err,
		)
	}

	metrics.RecordAuthOperation(
		context.Background(),
		AuthMetricOperationRefresh,
		MetricOutcomeSuccess,
		1500*time.Millisecond,
	)

	request := httptest.NewRequest(
		http.MethodGet,
		metricsPath,
		nil,
	)

	response := httptest.NewRecorder()

	runtime.server.Handler.ServeHTTP(
		response,
		request,
	)

	if response.Code != http.StatusOK {
		t.Fatalf(
			"metrics endpoint status = %d, expected %d",
			response.Code,
			http.StatusOK,
		)
	}

	body := response.Body.String()

	requireMetricsOutputContains(
		t,
		body,
		"identity_auth_operations_total",
	)

	requireMetricsOutputContains(
		t,
		body,
		`operation="refresh"`,
	)

	requireMetricsOutputContains(
		t,
		body,
		`outcome="success"`,
	)

	requireMetricsOutputContains(
		t,
		body,
		"identity_auth_duration_seconds",
	)

	requireMetricsOutputContains(
		t,
		body,
		`le="0.005"`,
	)

	requireMetricsOutputContains(
		t,
		body,
		`le="0.01"`,
	)

	requireMetricsOutputContains(
		t,
		body,
		`le="0.025"`,
	)

	requireMetricsOutputContains(
		t,
		body,
		`le="0.05"`,
	)

	requireMetricsOutputContains(
		t,
		body,
		`le="0.1"`,
	)

	requireMetricsOutputContains(
		t,
		body,
		`le="0.25"`,
	)

	requireMetricsOutputContains(
		t,
		body,
		`le="0.5"`,
	)

	requireMetricsOutputContains(
		t,
		body,
		`le="1"`,
	)

	requireMetricsOutputContains(
		t,
		body,
		`le="2.5"`,
	)

	requireMetricsOutputContains(
		t,
		body,
		`le="5"`,
	)

	requireMetricsOutputContains(
		t,
		body,
		`le="10"`,
	)

	requireMetricsOutputContains(
		t,
		body,
		"target_info",
	)

	requireMetricsOutputContains(
		t,
		body,
		`service_name="identity-service"`,
	)
}

func TestNewMetricsRuntimeRejectsInvalidConfiguration(
	t *testing.T,
) {
	tests := []struct {
		name        string
		serviceName string
		address     string
	}{
		{
			name:        "blank service name",
			serviceName: "   ",
			address:     ":9090",
		},
		{
			name:        "blank metrics address",
			serviceName: "identity-service",
			address:     "   ",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			runtime, err := NewMetricsRuntime(
				testCase.serviceName,
				testCase.address,
			)

			if err == nil {
				t.Fatal(
					"NewMetricsRuntime() accepted invalid configuration",
				)
			}

			if runtime != nil {
				t.Fatal(
					"NewMetricsRuntime() returned runtime for invalid configuration",
				)
			}
		})
	}
}

func TestMetricsRuntimeNilShutdownIsSafe(
	t *testing.T,
) {
	var runtime *MetricsRuntime

	if err := runtime.Shutdown(
		context.Background(),
	); err != nil {
		t.Fatalf(
			"Shutdown() returned an error: %v",
			err,
		)
	}
}

func requireMetricsOutputContains(
	t *testing.T,
	body string,
	expected string,
) {
	t.Helper()

	if !strings.Contains(
		body,
		expected,
	) {
		t.Fatalf(
			"metrics output does not contain %q\n\n%s",
			expected,
			body,
		)
	}
}

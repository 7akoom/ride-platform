package observability

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

type testOutboxMetricsSource struct {
	pending     int64
	age         float64
	err         error
	hasDeadline bool
}

func (s *testOutboxMetricsSource) PendingStats(ctx context.Context) (int64, float64, error) {
	deadline, ok := ctx.Deadline()
	s.hasDeadline = ok && time.Until(deadline) <= outboxMetricsQueryTimeout
	return s.pending, s.age, s.err
}

func TestOutboxMetricsReflectBacklogAndOmitFailedSamples(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	source := &testOutboxMetricsSource{pending: 4, age: 120}
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	if err := registerOutboxMetrics(provider.Meter("test"), source, logger); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		pending int64
		age     float64
		err     error
	}{
		{"backlog", 4, 120, nil},
		{"drained", 0, 0, nil},
		{"query_failed", 0, 0, errors.New("database error detail must not enter logs")},
		{"recovered", 2, 60, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source.pending, source.age, source.err = tc.pending, tc.age, tc.err
			var data metricdata.ResourceMetrics
			if err := reader.Collect(context.Background(), &data); err != nil {
				t.Fatal(err)
			}
			if !source.hasDeadline {
				t.Fatal("collection query must have a bounded timeout")
			}
			points := 0
			for _, scope := range data.ScopeMetrics {
				for _, m := range scope.Metrics {
					switch gauge := m.Data.(type) {
					case metricdata.Gauge[int64]:
						for _, p := range gauge.DataPoints {
							points++
							if m.Name != "identity.outbox.pending" || p.Value != tc.pending || p.Attributes.Len() != 0 {
								t.Fatal("incorrect pending gauge")
							}
						}
					case metricdata.Gauge[float64]:
						for _, p := range gauge.DataPoints {
							points++
							if m.Name != "identity.outbox.oldest_pending.age" || p.Value != tc.age || p.Attributes.Len() != 0 {
								t.Fatal("incorrect age gauge")
							}
						}
					}
				}
			}
			if tc.err == nil && points != 2 {
				t.Fatal("expected both backlog gauges")
			}
			if tc.err != nil && points != 0 {
				t.Fatal("failed collection must not emit zeros or stale values")
			}
		})
	}
	if !strings.Contains(logs.String(), "outbox metrics collection failed") || strings.Contains(logs.String(), "database error detail") {
		t.Fatal("collection failure must be logged without raw database errors")
	}
}

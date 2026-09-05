package observability

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/metric"
)

const outboxMetricsQueryTimeout = 3 * time.Second

type OutboxMetricsSource interface {
	PendingStats(context.Context) (pending int64, oldestAgeSeconds float64, err error)
}

func (r *MetricsRuntime) RegisterOutboxMetrics(source OutboxMetricsSource, logger *slog.Logger) error {
	if r == nil || r.meterProvider == nil {
		return errors.New("metrics runtime is not configured")
	}
	return registerOutboxMetrics(r.meterProvider.Meter("identity-service/outbox"), source, logger)
}

func registerOutboxMetrics(meter metric.Meter, source OutboxMetricsSource, logger *slog.Logger) error {
	if source == nil || logger == nil {
		return errors.New("outbox metrics source and logger are required")
	}
	pending, err := meter.Int64ObservableGauge("identity.outbox.pending",
		metric.WithDescription("Unpublished outbox events, including leased events and scheduled retries."))
	if err != nil {
		return fmt.Errorf("create outbox pending gauge: %w", err)
	}
	oldestAge, err := meter.Float64ObservableGauge("identity.outbox.oldest_pending.age",
		metric.WithUnit("s"), metric.WithDescription("Age of the oldest unpublished outbox event; zero when empty."))
	if err != nil {
		return fmt.Errorf("create outbox oldest pending age gauge: %w", err)
	}
	_, err = meter.RegisterCallback(func(ctx context.Context, observer metric.Observer) error {
		queryCtx, cancel := context.WithTimeout(ctx, outboxMetricsQueryTimeout)
		defer cancel()
		count, age, err := source.PendingStats(queryCtx)
		if err != nil {
			logger.ErrorContext(ctx, "outbox metrics collection failed")
			return nil
		}
		observer.ObserveInt64(pending, count)
		observer.ObserveFloat64(oldestAge, age)
		return nil
	}, pending, oldestAge)
	if err != nil {
		return fmt.Errorf("register outbox metrics callback: %w", err)
	}
	return nil
}

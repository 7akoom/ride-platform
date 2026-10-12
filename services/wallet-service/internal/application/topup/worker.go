package topup

import (
	"context"
	"log/slog"
	"time"
)

// Worker settles the top-ups still pending, asking their providers, every
// interval. It is what credits a paid top-up whose notification was lost
// (or never sent: ZainCash's test environment has no webhooks) when the
// customer never came back to the app.
type Worker struct {
	service  Service
	interval time.Duration
	logger   *slog.Logger
}

func NewWorker(service Service, interval time.Duration, logger *slog.Logger) *Worker {
	switch {
	case service == nil:
		panic("topup service is required")
	case interval <= 0:
		panic("the top-up sweep interval must be positive")
	case logger == nil:
		panic("logger is required")
	}

	return &Worker{service: service, interval: interval, logger: logger}
}

// Run sweeps until ctx ends.
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		settled, err := w.service.SettlePending(ctx)

		switch {
		case err != nil && ctx.Err() == nil:
			w.logger.Error("pending top-ups could not be swept; will retry", "error", err)
		case settled > 0:
			w.logger.Info("pending top-ups settled", "count", settled)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

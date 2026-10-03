package incentives

import (
	"context"
	"log/slog"
	"time"
)

const settleLease = 5 * time.Minute

// Worker pays ended campaigns, every interval.
type Worker struct {
	service  *Service
	interval time.Duration
	logger   *slog.Logger
}

func NewWorker(service *Service, interval time.Duration, logger *slog.Logger) *Worker {
	switch {
	case service == nil:
		panic("incentive service is required")
	case interval <= 0:
		panic("the incentive settle interval must be positive")
	case logger == nil:
		panic("logger is required")
	}

	return &Worker{service: service, interval: interval, logger: logger}
}

// Run settles until ctx ends; one campaign after another while any is due.
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		for ctx.Err() == nil {
			done, err := w.service.SettleDue(ctx, settleLease)
			if err != nil {
				if ctx.Err() == nil {
					w.logger.Error("an incentive campaign could not be settled; will retry", "error", err)
				}

				break
			}

			if !done {
				break
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

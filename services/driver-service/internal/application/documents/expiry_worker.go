package documents

import (
	"context"
	"log/slog"
	"time"
)

const expiryBatch = 200

// ExpiryWorker sends expiry reminders and takes drivers offline when a
// required document runs out, every interval.
type ExpiryWorker struct {
	service  *Service
	interval time.Duration
	logger   *slog.Logger
}

func NewExpiryWorker(service *Service, interval time.Duration, logger *slog.Logger) *ExpiryWorker {
	switch {
	case service == nil:
		panic("document service is required")
	case interval <= 0:
		panic("the document expiry interval must be positive")
	case logger == nil:
		panic("logger is required")
	}

	return &ExpiryWorker{service: service, interval: interval, logger: logger}
}

// Run checks until ctx ends. A full batch is followed at once by another.
func (w *ExpiryWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		for {
			round, err := w.service.RunExpiry(ctx, expiryBatch)
			if err != nil {
				if ctx.Err() == nil {
					w.logger.Error("driver document expiry check failed", "error", err)
				}

				break
			}

			if round.Reminded+round.Expired > 0 {
				w.logger.Info("driver document expiry check",
					"reminded", round.Reminded, "expired", round.Expired, "taken_offline", round.TookOffline)
			}

			if round.Reminded < expiryBatch && round.Expired < expiryBatch {
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

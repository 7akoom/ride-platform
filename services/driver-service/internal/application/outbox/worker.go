package outbox

import (
	"context"
	"log/slog"
	"time"
)

type ProcessorRunner interface {
	ProcessOnce(
		ctx context.Context,
	) (ProcessResult, error)
}

type WorkerConfig struct {
	PollInterval time.Duration
}

type Worker struct {
	processor ProcessorRunner
	logger    *slog.Logger
	config    WorkerConfig
}

func NewWorker(
	processor ProcessorRunner,
	logger *slog.Logger,
	config WorkerConfig,
) *Worker {
	if processor == nil {
		panic("outbox processor is required")
	}

	if logger == nil {
		panic("outbox logger is required")
	}

	if config.PollInterval <= 0 {
		panic("outbox poll interval must be positive")
	}

	return &Worker{
		processor: processor,
		logger:    logger,
		config:    config,
	}
}

func (w *Worker) Run(
	ctx context.Context,
) error {
	for {
		if ctx.Err() != nil {
			return nil
		}

		result, err := w.processor.ProcessOnce(
			ctx,
		)

		if ctx.Err() != nil {
			return nil
		}

		if err != nil {
			w.logger.ErrorContext(
				ctx,
				"outbox processing cycle completed with errors",
				"error",
				err,
				"claimed",
				result.Claimed,
				"published",
				result.Published,
				"retry_scheduled",
				result.RetryScheduled,
				"lost_claims",
				result.LostClaims,
			)
		}

		timer := time.NewTimer(
			w.config.PollInterval,
		)

		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}

			return nil

		case <-timer.C:
		}
	}
}

package outbox

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type Clock interface {
	Now() time.Time
}

type ProcessorConfig struct {
	BatchSize         int
	LeaseDuration     time.Duration
	InitialRetryDelay time.Duration
	MaxRetryDelay     time.Duration
}

type ProcessResult struct {
	Claimed        int
	Published      int
	RetryScheduled int
	LostClaims     int
}

type Processor struct {
	store     Store
	publisher Publisher
	clock     Clock
	config    ProcessorConfig
}

func NewProcessor(
	store Store,
	publisher Publisher,
	clock Clock,
	config ProcessorConfig,
) *Processor {
	if store == nil {
		panic("outbox store is required")
	}

	if publisher == nil {
		panic("outbox publisher is required")
	}

	if clock == nil {
		panic("outbox clock is required")
	}

	if config.BatchSize <= 0 {
		panic("outbox batch size must be positive")
	}

	if config.LeaseDuration <= 0 {
		panic("outbox lease duration must be positive")
	}

	if config.InitialRetryDelay <= 0 {
		panic("outbox initial retry delay must be positive")
	}

	if config.MaxRetryDelay <= 0 {
		panic("outbox maximum retry delay must be positive")
	}

	if config.InitialRetryDelay >
		config.MaxRetryDelay {
		panic(
			"outbox initial retry delay cannot exceed maximum retry delay",
		)
	}

	return &Processor{
		store:     store,
		publisher: publisher,
		clock:     clock,
		config:    config,
	}
}

func (p *Processor) ProcessOnce(
	ctx context.Context,
) (ProcessResult, error) {
	claimedAt := p.clock.Now().UTC()

	events, err := p.store.ClaimPending(
		ctx,
		ClaimPendingInput{
			ClaimedAt:     claimedAt,
			LeaseDuration: p.config.LeaseDuration,
			Limit:         p.config.BatchSize,
		},
	)
	if err != nil {
		return ProcessResult{},
			fmt.Errorf(
				"claim pending outbox events: %w",
				err,
			)
	}

	result := ProcessResult{
		Claimed: len(events),
	}

	var processingErrors []error

	for _, event := range events {
		if err := ctx.Err(); err != nil {
			processingErrors = append(
				processingErrors,
				err,
			)
			break
		}

		message := Message{
			ID:            event.ID,
			AggregateType: event.AggregateType,
			AggregateID:   event.AggregateID,
			EventType:     event.EventType,
			SchemaVersion: event.SchemaVersion,
			Payload: append(
				[]byte(nil),
				event.Payload...,
			),
			OccurredAt: event.OccurredAt,
		}

		publishErr := p.publisher.Publish(
			ctx,
			message,
		)
		if publishErr != nil {
			failedAt := p.clock.Now().UTC()

			retryAt := failedAt.Add(
				p.retryDelay(
					event.PublishAttempts,
				),
			)

			updated, markErr := p.store.MarkFailed(
				ctx,
				MarkFailedInput{
					EventID:      event.ID,
					ClaimToken:   event.ClaimToken,
					FailedAt:     failedAt,
					RetryAt:      retryAt,
					ErrorMessage: publishErr.Error(),
				},
			)
			if markErr != nil {
				processingErrors = append(
					processingErrors,
					fmt.Errorf(
						"mark outbox event %q failed after publish error: %w",
						event.ID,
						markErr,
					),
				)

				continue
			}

			if !updated {
				result.LostClaims++

				processingErrors = append(
					processingErrors,
					fmt.Errorf(
						"outbox event %q lost claim after publish failure",
						event.ID,
					),
				)

				continue
			}

			result.RetryScheduled++

			processingErrors = append(
				processingErrors,
				fmt.Errorf(
					"publish outbox event %q: %w",
					event.ID,
					publishErr,
				),
			)

			continue
		}

		publishedAt := p.clock.Now().UTC()

		updated, err := p.store.MarkPublished(
			ctx,
			MarkPublishedInput{
				EventID:     event.ID,
				ClaimToken:  event.ClaimToken,
				PublishedAt: publishedAt,
			},
		)
		if err != nil {
			processingErrors = append(
				processingErrors,
				fmt.Errorf(
					"mark outbox event %q published: %w",
					event.ID,
					err,
				),
			)

			continue
		}

		if !updated {
			result.LostClaims++

			processingErrors = append(
				processingErrors,
				fmt.Errorf(
					"outbox event %q lost claim after successful publish",
					event.ID,
				),
			)

			continue
		}

		result.Published++
	}

	return result,
		errors.Join(processingErrors...)
}

func (p *Processor) retryDelay(
	publishAttempts int,
) time.Duration {
	if publishAttempts <= 1 {
		return p.config.InitialRetryDelay
	}

	delay := p.config.InitialRetryDelay

	for attempt := 1; attempt < publishAttempts; attempt++ {
		if delay >= p.config.MaxRetryDelay {
			return p.config.MaxRetryDelay
		}

		if delay >
			p.config.MaxRetryDelay/2 {
			return p.config.MaxRetryDelay
		}

		delay *= 2
	}

	if delay > p.config.MaxRetryDelay {
		return p.config.MaxRetryDelay
	}

	return delay
}

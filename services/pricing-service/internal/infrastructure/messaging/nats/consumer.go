package nats

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// MessageHandler processes one event. Returning nil acks the message;
// a non-nil error naks it, so JetStream redelivers it (the handler must
// be safe to run more than once for the same event).
//
// If the returned error implements RetryDelay() time.Duration, the
// message is redelivered after that delay instead of immediately — a
// transient failure retries on a timer instead of a hot loop.
type MessageHandler func(ctx context.Context, subject string, data []byte) error

// retryDelayer is satisfied by errors that ask for delayed redelivery.
type retryDelayer interface {
	RetryDelay() time.Duration
}

const consumerAckWait = 30 * time.Second

// Subscription is a running durable consumer. Stop unregisters the
// in-process delivery loop; it does not delete the durable consumer
// itself, so redelivery picks up where it left off on the next Subscribe.
type Subscription struct {
	consumeCtx jetstream.ConsumeContext
}

func (s *Subscription) Stop() {
	if s == nil || s.consumeCtx == nil {
		return
	}

	s.consumeCtx.Stop()
}

// SubscribeDurable binds (creating it on first run) a durable pull
// consumer named durableName on streamName, filtered to filterSubjects,
// and starts delivering matching messages to handler. The durable name
// and the stream must already exist — streams are created by each
// publishing service's compose bootstrap container, not here.
func SubscribeDurable(
	ctx context.Context,
	js jetstream.JetStream,
	streamName string,
	durableName string,
	filterSubjects []string,
	handler MessageHandler,
	logger *slog.Logger,
) (*Subscription, error) {
	consumer, err := js.CreateOrUpdateConsumer(
		ctx,
		streamName,
		jetstream.ConsumerConfig{
			Durable:   durableName,
			AckPolicy: jetstream.AckExplicitPolicy,
			// A brand-new durable starts from now, not from the stream's
			// history: replaying every old trip.completed would price (and
			// notify riders about) trips that finished long before this
			// consumer existed. Once created, the durable resumes exactly
			// where it left off, so nothing is missed across restarts.
			DeliverPolicy:  jetstream.DeliverNewPolicy,
			FilterSubjects: filterSubjects,
			AckWait:        consumerAckWait,
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"create/update durable consumer %q on stream %q: %w",
			durableName,
			streamName,
			err,
		)
	}

	consumeCtx, err := consumer.Consume(func(msg jetstream.Msg) {
		if err := handler(ctx, msg.Subject(), msg.Data()); err != nil {
			var delayed retryDelayer

			if errors.As(err, &delayed) {
				logger.DebugContext(
					ctx,
					"event handler asked for delayed redelivery",
					"subject", msg.Subject(),
					"delay", delayed.RetryDelay(),
				)

				if nakErr := msg.NakWithDelay(delayed.RetryDelay()); nakErr != nil {
					logger.ErrorContext(ctx, "failed to nak message with delay", "error", nakErr)
				}

				return
			}

			logger.ErrorContext(
				ctx,
				"event handler failed; message will be redelivered",
				"subject", msg.Subject(),
				"error", err,
			)

			if nakErr := msg.Nak(); nakErr != nil {
				logger.ErrorContext(ctx, "failed to nak message", "error", nakErr)
			}

			return
		}

		if ackErr := msg.Ack(); ackErr != nil {
			logger.ErrorContext(ctx, "failed to ack message", "error", ackErr)
		}
	})
	if err != nil {
		return nil, fmt.Errorf(
			"start consuming stream %q with consumer %q: %w",
			streamName,
			durableName,
			err,
		)
	}

	return &Subscription{consumeCtx: consumeCtx}, nil
}

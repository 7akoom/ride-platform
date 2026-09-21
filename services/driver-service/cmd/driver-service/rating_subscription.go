package main

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/7akoom/ride-platform/services/driver-service/internal/application/ratings"
	natsinfra "github.com/7akoom/ride-platform/services/driver-service/internal/infrastructure/messaging/nats"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	// tripEventsStream belongs to trip-service (its compose bootstrap creates it); this
	// service only binds a durable consumer to it.
	tripEventsStream = "TRIP_EVENTS"

	// tripRatedDurable is this service's own durable consumer name. It must stay stable across
	// restarts so redelivery resumes where it left off.
	tripRatedDurable = "driver-trip-rated"

	streamWaitAttempts = 30
	streamWaitInterval = 2 * time.Second
)

// subscribeTripRatings binds the durable consumer, waiting for trip-service to have created
// the stream if it has not yet, so the start order of the containers does not matter.
func subscribeTripRatings(
	ctx context.Context,
	js jetstream.JetStream,
	handler *ratings.Handler,
	logger *slog.Logger,
) (*natsinfra.Subscription, error) {
	var lastErr error

	for attempt := 1; attempt <= streamWaitAttempts; attempt++ {
		subscription, err := natsinfra.SubscribeDurable(
			ctx,
			js,
			tripEventsStream,
			tripRatedDurable,
			[]string{ratings.SubjectTripRated},
			handler.Handle,
			logger,
		)
		if err == nil {
			return subscription, nil
		}

		lastErr = err

		if !errors.Is(err, jetstream.ErrStreamNotFound) {
			return nil, err
		}

		logger.Warn("the trip event stream does not exist yet; waiting for trip-service",
			"stream", tripEventsStream,
			"attempt", attempt,
		)

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(streamWaitInterval):
		}
	}

	return nil, lastErr
}

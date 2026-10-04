package main

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/7akoom/ride-platform/services/driver-service/internal/application/erasure"
	natsinfra "github.com/7akoom/ride-platform/services/driver-service/internal/infrastructure/messaging/nats"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	// identityEventsStream belongs to identity-service; this service only
	// binds a durable consumer to it.
	identityEventsStream = "IDENTITY_EVENTS"

	// identityErasureDurable must stay stable so redelivery resumes where it
	// left off.
	identityErasureDurable = "driver-account-erasure"

	identityStreamWaitAttempts = 30
	identityStreamWaitInterval = 2 * time.Second
)

// subscribeAccountErasure binds the durable consumer for deleted accounts,
// waiting for identity-service to have created the stream.
func subscribeAccountErasure(
	ctx context.Context,
	js jetstream.JetStream,
	handler *erasure.Handler,
	logger *slog.Logger,
) (*natsinfra.Subscription, error) {
	var lastErr error

	for attempt := 1; attempt <= identityStreamWaitAttempts; attempt++ {
		subscription, err := natsinfra.SubscribeDurable(
			ctx,
			js,
			identityEventsStream,
			identityErasureDurable,
			erasure.Subjects,
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

		logger.Warn("the identity event stream does not exist yet; waiting for identity-service",
			"stream", identityEventsStream,
			"attempt", attempt,
		)

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(identityStreamWaitInterval):
		}
	}

	return nil, lastErr
}

package events

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// deliveredMemory bounds how many delivered event ids are remembered.
const deliveredMemory = 1024

// retryLaterError tells the JetStream consumer to redeliver the message
// after a delay instead of immediately, so a failing SMS gateway is not
// hammered in a hot loop.
type retryLaterError struct {
	delay time.Duration
	cause error
}

func (e *retryLaterError) Error() string {
	return fmt.Sprintf("retry in %s: %v", e.delay, e.cause)
}

func (e *retryLaterError) Unwrap() error {
	return e.cause
}

// RetryDelay is discovered by the NATS consumer through an interface
// check, so the infrastructure layer never imports this package.
func (e *retryLaterError) RetryDelay() time.Duration {
	return e.delay
}

// sosDelivery decides what happens after an SOS alert is built: send it,
// retry it on a timer if every channel failed, stop retrying eventually,
// and never send the same event's alert twice.
type sosDelivery struct {
	alerter       OperatorAlerter // nil when no channel is configured
	retryInterval time.Duration
	giveUpAfter   time.Duration
	now           func() time.Time
	logger        *slog.Logger

	mu        sync.Mutex
	delivered map[string]struct{}
	order     []string
}

func newSOSDelivery(logger *slog.Logger) *sosDelivery {
	return &sosDelivery{
		now:       time.Now,
		logger:    logger,
		delivered: make(map[string]struct{}),
	}
}

// deliver returns nil when the alert was delivered, or there is nothing
// more worth doing, and a retryLaterError when it should be tried again.
//
// eventID makes delivery once-only within this process: the handler may
// run again for the same event (for example because sending the user's
// confirmation failed afterwards), and operators must not be paged twice.
func (d *sosDelivery) deliver(ctx context.Context, eventID string, alert SOSAlert) error {
	if d.alerter == nil {
		// Loud on purpose: an SOS that nobody at the company hears about is
		// an operational failure, not something to log at warning level.
		d.logger.ErrorContext(ctx, "SOS raised but no operator alert channel is configured; NOBODY at the company was alerted",
			"alert_id", alert.AlertID,
			"trip_id", alert.TripID,
			"triggered_by", alert.TriggeredBy,
		)

		return nil
	}

	if d.alreadyDelivered(eventID) {
		d.logger.InfoContext(ctx, "SOS operator alert already delivered for this event; not sending it again",
			"alert_id", alert.AlertID,
		)

		return nil
	}

	err := d.alerter.Alert(ctx, alert)
	if err == nil {
		d.remember(eventID)

		return nil
	}

	if !alert.TriggeredAt.IsZero() && d.now().Sub(alert.TriggeredAt) > d.giveUpAfter {
		d.logger.ErrorContext(ctx, "could not alert operators of an SOS within the retry window; giving up",
			"alert_id", alert.AlertID,
			"trip_id", alert.TripID,
			"triggered_at", alert.TriggeredAt,
			"error", err,
		)

		return nil
	}

	d.logger.WarnContext(ctx, "SOS operator alert failed on every channel; will retry",
		"alert_id", alert.AlertID,
		"trip_id", alert.TripID,
		"retry_in", d.retryInterval,
		"error", err,
	)

	return &retryLaterError{delay: d.retryInterval, cause: err}
}

func (d *sosDelivery) alreadyDelivered(eventID string) bool {
	if eventID == "" {
		return false
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	_, found := d.delivered[eventID]

	return found
}

func (d *sosDelivery) remember(eventID string) {
	if eventID == "" {
		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if _, found := d.delivered[eventID]; found {
		return
	}

	d.delivered[eventID] = struct{}{}
	d.order = append(d.order, eventID)

	if len(d.order) > deliveredMemory {
		oldest := d.order[0]
		d.order = d.order[1:]
		delete(d.delivered, oldest)
	}
}

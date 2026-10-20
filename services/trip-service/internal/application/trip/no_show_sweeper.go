package trip

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

const (
	// noShowSweepBatch is how many waiting trips one round looks at.
	noShowSweepBatch = 50
	// minNoShowWait: no card waits less, so trips the captain reached sooner are
	// not looked at yet.
	minNoShowWait = time.Minute
	// noShowReason is what a trip cancelled by the sweeper says.
	noShowReason = "the rider did not come to the pickup in time"
)

// WaitingTrips lists the trips whose captain is at the pickup (accepted and
// arrived, not started) since before arrivedBefore, the longest waiting first.
type WaitingTrips interface {
	ListWaitingAtPickup(ctx context.Context, arrivedBefore time.Time, limit int) ([]string, error)
}

// NoShowSweeper cancels the trips whose rider did not come once the trip's rate
// card's wait ran out, as a no-show: the rider pays the card's no-show fee
// (pricing-service, then wallet-service), which goes to the captain.
type NoShowSweeper struct {
	waiting  WaitingTrips
	trips    Service
	interval time.Duration
	logger   *slog.Logger
	now      func() time.Time
}

func NewNoShowSweeper(waiting WaitingTrips, trips Service, interval time.Duration, logger *slog.Logger) *NoShowSweeper {
	switch {
	case waiting == nil:
		panic("waiting trips are required")
	case trips == nil:
		panic("trip service is required")
	case interval <= 0:
		panic("the no-show sweep interval must be positive")
	case logger == nil:
		panic("logger is required")
	}

	return &NoShowSweeper{waiting: waiting, trips: trips, interval: interval, logger: logger, now: time.Now}
}

// Run sweeps every interval until ctx ends.
func (w *NoShowSweeper) Run(ctx context.Context) error {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		if _, err := w.Tick(ctx); err != nil && ctx.Err() == nil {
			w.logger.Error("the no-show sweep failed this round", "error", err)
		}

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// Tick cancels the trips whose wait ran out; it returns how many.
func (w *NoShowSweeper) Tick(ctx context.Context) (int, error) {
	ids, err := w.waiting.ListWaitingAtPickup(ctx, w.now().Add(-minNoShowWait), noShowSweepBatch)
	if err != nil {
		return 0, fmt.Errorf("list trips waiting at the pickup: %w", err)
	}

	cancelled := 0

	for _, id := range ids {
		// CancelTrip checks the trip's own wait (its rate card's) against the
		// locked row: too early is the usual answer, and not an error here.
		_, err := w.trips.CancelTrip(ctx, CancelInput{TripID: id, Reason: noShowReason, By: CancelledBySystem, RiderNoShow: true})

		switch {
		case err == nil:
			cancelled++
			w.logger.Info("trip cancelled: the rider did not come", "trip_id", id)
		case errors.Is(err, ErrNoShowTooEarly):
		default:
			w.logger.Warn("a waiting trip could not be cancelled as a no-show", "trip_id", id, "error", err)
		}
	}

	return cancelled, nil
}

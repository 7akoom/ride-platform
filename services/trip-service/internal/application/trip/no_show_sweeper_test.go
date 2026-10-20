package trip_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

type waitingFake struct {
	ids    []string
	before time.Time
	limit  int
}

func (f *waitingFake) ListWaitingAtPickup(_ context.Context, arrivedBefore time.Time, limit int) ([]string, error) {
	f.before, f.limit = arrivedBefore, limit

	return f.ids, nil
}

// cancelOnly is a trip.Service that only cancels; the sweeper needs nothing else.
type cancelOnly struct {
	trip.Service

	answers map[string]error
	asked   []trip.CancelInput
}

func (c *cancelOnly) CancelTrip(_ context.Context, input trip.CancelInput) (trip.Trip, error) {
	c.asked = append(c.asked, input)

	return trip.Trip{ID: input.TripID, Status: trip.StatusCancelled}, c.answers[input.TripID]
}

func TestTheNoShowSweeperCancelsTheTripsWhoseWaitRanOut(t *testing.T) {
	waiting := &waitingFake{ids: []string{"due", "early", "gone"}}
	trips := &cancelOnly{answers: map[string]error{
		"early": trip.ErrNoShowTooEarly,
		"gone":  errors.New("trip is not active"),
	}}
	sweeper := trip.NewNoShowSweeper(waiting, trips, time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))

	cancelled, err := sweeper.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if cancelled != 1 || len(trips.asked) != 3 {
		t.Fatalf("cancelled %d of %d asked", cancelled, len(trips.asked))
	}

	for _, input := range trips.asked {
		if input.By != trip.CancelledBySystem || !input.RiderNoShow {
			t.Fatalf("cancelled as %+v, want the system and a no-show", input)
		}
	}

	if time.Since(waiting.before) < time.Minute || waiting.limit <= 0 {
		t.Fatalf("asked for trips arrived before %v (limit %d)", waiting.before, waiting.limit)
	}
}

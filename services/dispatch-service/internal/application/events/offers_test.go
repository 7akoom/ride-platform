package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/dispatch-service/internal/application/dispatch"
)

type offersDispatcher struct {
	result dispatch.Result
	err    error
	calls  int
}

func (f *offersDispatcher) DispatchTrip(context.Context, string, float64) (dispatch.Result, error) {
	f.calls++

	return f.result, f.err
}

type offersCanceller struct {
	status    string
	cancelled []string
}

func (f *offersCanceller) GetTrip(context.Context, string) (dispatch.TripInfo, error) {
	return dispatch.TripInfo{ID: "trip-1", Status: f.status}, nil
}

func (f *offersCanceller) CancelTrip(_ context.Context, tripID string, _ string) error {
	f.cancelled = append(f.cancelled, tripID)

	return nil
}

func offersEvent(t *testing.T, occurredAt time.Time) []byte {
	t.Helper()

	data, err := json.Marshal(Envelope{EventID: "e1", EventType: "trip.requested", AggregateType: "trip", AggregateID: "trip-1", OccurredAt: occurredAt})
	if err != nil {
		t.Fatal(err)
	}

	return data
}

func newOffersHandler(dispatcher dispatch.Service, canceller TripCanceller, retry time.Duration) *Handler {
	return NewHandler(dispatcher, canceller, retry, 2*time.Minute, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func retryOf(t *testing.T, err error) (time.Duration, error) {
	t.Helper()

	var retry *retryLaterError
	if !errors.As(err, &retry) {
		t.Fatalf("expected a delayed retry, got %v", err)
	}

	return retry.RetryDelay(), retry.Unwrap()
}

func TestATripThatWasOfferedIsLookedAtAgainSoonNotAcknowledged(t *testing.T) {
	dispatcher := &offersDispatcher{result: dispatch.Result{TripID: "trip-1", DriverID: "d1", Offered: true}}
	handler := newOffersHandler(dispatcher, &offersCanceller{status: "requested"}, 5*time.Second)

	err := handler.Handle(context.Background(), SubjectTripRequested, offersEvent(t, time.Now()))

	delay, cause := retryOf(t, err)
	if delay != maxOfferPollInterval || !errors.Is(cause, errOfferAwaitingAnswer) {
		t.Errorf("expected a look again in %v because the driver has not answered, got %v: %v", maxOfferPollInterval, delay, cause)
	}
}

func TestTheWaitIsNeverLongerThanTheRetryInterval(t *testing.T) {
	dispatcher := &offersDispatcher{result: dispatch.Result{Offered: true}}
	handler := newOffersHandler(dispatcher, &offersCanceller{}, time.Second)

	delay, _ := retryOf(t, handler.Handle(context.Background(), SubjectTripRequested, offersEvent(t, time.Now())))
	if delay != time.Second {
		t.Errorf("a retry interval shorter than the poll interval must win, got %v", delay)
	}
}

func TestAnotherDriversLiveOfferIsWaitedOutQuietly(t *testing.T) {
	dispatcher := &offersDispatcher{err: fmt.Errorf("context: %w", dispatch.ErrOfferPending)}
	handler := newOffersHandler(dispatcher, &offersCanceller{}, 5*time.Second)

	delay, cause := retryOf(t, handler.Handle(context.Background(), SubjectTripRequested, offersEvent(t, time.Now())))
	if delay != maxOfferPollInterval || !errors.Is(cause, dispatch.ErrOfferPending) {
		t.Errorf("expected a look again in %v for a live offer, got %v: %v", maxOfferPollInterval, delay, cause)
	}
}

func TestATripThatWasAssignedIsAcknowledgedAsBefore(t *testing.T) {
	dispatcher := &offersDispatcher{result: dispatch.Result{TripID: "trip-1", DriverID: "d1"}}
	handler := newOffersHandler(dispatcher, &offersCanceller{}, 5*time.Second)

	if err := handler.Handle(context.Background(), SubjectTripRequested, offersEvent(t, time.Now())); err != nil {
		t.Errorf("an assigned trip is done: %v", err)
	}
}

func TestNoDriverYetKeepsTheSlowRetryInterval(t *testing.T) {
	for _, refusal := range []error{dispatch.ErrNoDriversNearby, dispatch.ErrNoDriversAvailable} {
		handler := newOffersHandler(&offersDispatcher{err: refusal}, &offersCanceller{}, 5*time.Second)

		delay, cause := retryOf(t, handler.Handle(context.Background(), SubjectTripRequested, offersEvent(t, time.Now())))
		if delay != 5*time.Second || !errors.Is(cause, refusal) {
			t.Errorf("%v: expected the 5s retry interval, got %v: %v", refusal, delay, cause)
		}
	}
}

func TestATripThatIsNoLongerWaitingIsAcknowledged(t *testing.T) {
	handler := newOffersHandler(&offersDispatcher{err: dispatch.ErrTripNotDispatchable}, &offersCanceller{}, 5*time.Second)

	if err := handler.Handle(context.Background(), SubjectTripRequested, offersEvent(t, time.Now())); err != nil {
		t.Errorf("a trip accepted or cancelled meanwhile is done: %v", err)
	}
}

func TestPastTheSearchWindowTheTripIsCancelledAndNothingIsOffered(t *testing.T) {
	dispatcher := &offersDispatcher{result: dispatch.Result{Offered: true}}
	canceller := &offersCanceller{status: "requested"}
	handler := newOffersHandler(dispatcher, canceller, 5*time.Second)

	err := handler.Handle(context.Background(), SubjectTripRequested, offersEvent(t, time.Now().Add(-3*time.Minute)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if dispatcher.calls != 0 || len(canceller.cancelled) != 1 {
		t.Errorf("the trip must be given up on without another offer: calls=%d cancelled=%v", dispatcher.calls, canceller.cancelled)
	}
}

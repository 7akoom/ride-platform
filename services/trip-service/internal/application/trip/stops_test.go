package trip_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

var twoStops = []trip.Stop{
	{Coordinates: trip.Coordinates{Latitude: 36.195, Longitude: 44.015}, Address: " Bakery "},
	{Coordinates: trip.Coordinates{Latitude: 36.198, Longitude: 44.018}, Address: "Pharmacy"},
}

func TestStopsAreCheckedAndKeptInOrder(t *testing.T) {
	reached := time.Now()
	withReached := append([]trip.Stop{}, twoStops...)
	withReached[0].ReachedAt = &reached

	got, err := trip.NormalizeStops(withReached)
	if err != nil || len(got) != 2 || got[0].Address != "Bakery" || got[0].ReachedAt != nil || got[1].Coordinates != twoStops[1].Coordinates {
		t.Fatalf("got %+v %v", got, err)
	}

	for name, c := range map[string]struct {
		stops []trip.Stop
		want  error
	}{
		"three":          {append(append([]trip.Stop{}, twoStops...), twoStops[0]), trip.ErrTooManyStops},
		"a bad point":    {[]trip.Stop{{Coordinates: trip.Coordinates{Latitude: 91}}}, trip.ErrInvalidLatitude},
		"a long address": {[]trip.Stop{{Address: strings.Repeat("a", 301)}}, trip.ErrAddressTooLong},
	} {
		if _, err := trip.NormalizeStops(c.stops); !errors.Is(err, c.want) {
			t.Errorf("%s: got %v, want %v", name, err, c.want)
		}
	}
}

func TestATripIsCreatedWithItsStops(t *testing.T) {
	repo := &fakeRepository{findActiveByRiderIDErr: trip.ErrTripNotFound}
	svc := newService(repo)

	input := validRequestInput()
	input.Stops = twoStops

	if _, err := svc.RequestTrip(context.Background(), input); err != nil {
		t.Fatal(err)
	}

	if stops := repo.createCalls[0].Stops; len(stops) != 2 || stops[0].Address != "Bakery" {
		t.Fatalf("created with %+v", stops)
	}

	input.Stops = append(append([]trip.Stop{}, twoStops...), twoStops[0])
	if _, err := svc.RequestTrip(context.Background(), input); !errors.Is(err, trip.ErrTooManyStops) || len(repo.createCalls) != 1 {
		t.Fatalf("three stops: %v", err)
	}
}

func TestAQuotedTripHasTheQuotesStops(t *testing.T) {
	quoted := []trip.Coordinates{{Latitude: 36.19502, Longitude: 44.01502}, {Latitude: 36.198, Longitude: 44.018}}

	for name, c := range map[string]struct {
		stops []trip.Stop
		ok    bool
	}{
		"the same":       {twoStops, true},
		"none":           {nil, false},
		"one":            {twoStops[:1], false},
		"in other order": {[]trip.Stop{twoStops[1], twoStops[0]}, false},
	} {
		svc, repo, book := quoteRig()
		book.quote.Stops = quoted

		input := quotedInput()
		input.Stops = c.stops

		_, err := svc.RequestTrip(context.Background(), input)
		if c.ok != (err == nil) || (!c.ok && !errors.Is(err, trip.ErrQuoteMismatch)) {
			t.Errorf("%s: %v", name, err)
		}

		if !c.ok && (len(repo.createCalls) != 0 || len(book.released) != 1) {
			t.Errorf("%s: created %d, released %v", name, len(repo.createCalls), book.released)
		}
	}
}

type fakeStopStore struct {
	marked []int
}

func (s *fakeStopStore) MarkStopReached(_ context.Context, tripID string, position int) (trip.Trip, error) {
	s.marked = append(s.marked, position)

	return trip.Trip{ID: tripID}, nil
}

type reachStop interface {
	ReachStop(ctx context.Context, tripID string, position int) (trip.Trip, error)
}

func stopRig(current trip.Trip) (reachStop, *fakeStopStore, *arrivalLocator) {
	repo := &fakeRepository{findByIDResult: current}
	store := &fakeStopStore{}
	// A few metres from the second stop.
	locator := &arrivalLocator{position: trip.DriverLocation{Latitude: 36.1981, Longitude: 44.0181}}

	svc, ok := trip.As[reachStop](trip.WithStopArrivals(newService(repo), locator, store))
	if !ok {
		panic("no ReachStop")
	}

	return svc, store, locator
}

func tripUnderWay() trip.Trip {
	return trip.Trip{ID: "trip-1", DriverID: "driver-1", Status: trip.StatusInProgress, Stops: append([]trip.Stop{}, twoStops...)}
}

func TestADriverAtAStopMarksIt(t *testing.T) {
	svc, store, locator := stopRig(tripUnderWay())

	// Stops may be reached in any order: a rider may skip one.
	if _, err := svc.ReachStop(context.Background(), "trip-1", 2); err != nil {
		t.Fatal(err)
	}

	if len(store.marked) != 1 || store.marked[0] != 2 || locator.asked[0] != "driver-1" {
		t.Fatalf("marked %v, asked %v", store.marked, locator.asked)
	}
}

func TestAStopReachedAlreadyIsNotMarkedAgain(t *testing.T) {
	current := tripUnderWay()
	reached := time.Now()
	current.Stops[1].ReachedAt = &reached

	svc, store, locator := stopRig(current)

	got, err := svc.ReachStop(context.Background(), "trip-1", 2)
	if err != nil || got.Stops[1].ReachedAt == nil || len(store.marked) != 0 || len(locator.asked) != 0 {
		t.Fatalf("got %+v %v, marked %v", got, err, store.marked)
	}
}

func TestReachingAStopRefusals(t *testing.T) {
	far := trip.DriverLocation{Latitude: 36.19, Longitude: 44.01}

	cases := map[string]struct {
		edit     func(*trip.Trip)
		position int
		located  *trip.DriverLocation
		posErr   error
		want     error
	}{
		"no such stop":     {position: 3, want: trip.ErrStopNotFound},
		"position zero":    {position: 0, want: trip.ErrStopNotFound},
		"a trip without":   {edit: func(t *trip.Trip) { t.Stops = nil }, position: 1, want: trip.ErrStopNotFound},
		"not started":      {edit: func(t *trip.Trip) { t.Status = trip.StatusAccepted }, position: 2, want: trip.ErrInvalidTransition},
		"completed":        {edit: func(t *trip.Trip) { t.Status = trip.StatusCompleted }, position: 2, want: trip.ErrInvalidTransition},
		"position unknown": {position: 2, posErr: trip.ErrDriverLocationUnavailable, want: trip.ErrArrivalPositionUnknown},
		"too far":          {position: 2, located: &far, want: trip.ErrTooFarFromStop},
	}

	for name, c := range cases {
		current := tripUnderWay()
		if c.edit != nil {
			c.edit(&current)
		}

		svc, store, locator := stopRig(current)
		locator.err = c.posErr

		if c.located != nil {
			locator.position = *c.located
		}

		if _, err := svc.ReachStop(context.Background(), "trip-1", c.position); !errors.Is(err, c.want) || len(store.marked) != 0 {
			t.Errorf("%s: got %v, want %v (marked %v)", name, err, c.want, store.marked)
		}
	}
}

package trip_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

type arrivalLocator struct {
	position trip.DriverLocation
	err      error
	asked    []string
}

func (l *arrivalLocator) DriverLocation(_ context.Context, driverID string) (trip.DriverLocation, error) {
	l.asked = append(l.asked, driverID)

	return l.position, l.err
}

type markDriverArrived interface {
	MarkDriverArrived(ctx context.Context, tripID string) (trip.Trip, error)
}

func arrivalRig(current trip.Trip) (markDriverArrived, *fakeRepository, *arrivalLocator) {
	repo := &fakeRepository{findByIDResult: current}
	locator := &arrivalLocator{position: trip.DriverLocation{Latitude: 36.1905, Longitude: 44.0102}}

	svc, ok := trip.As[markDriverArrived](trip.WithDriverArrival(newService(repo), locator, repo))
	if !ok {
		panic("no MarkDriverArrived")
	}

	return svc, repo, locator
}

func acceptedTrip() trip.Trip {
	return trip.Trip{
		ID: "trip-1", DriverID: "driver-1", Status: trip.StatusAccepted,
		Pickup: trip.Coordinates{Latitude: 36.19, Longitude: 44.01},
	}
}

func TestADriverAtThePickupMarksArrival(t *testing.T) {
	svc, repo, locator := arrivalRig(acceptedTrip())

	got, err := svc.MarkDriverArrived(context.Background(), "trip-1")
	if err != nil || got.ArrivedAt == nil {
		t.Fatalf("got %+v %v", got, err)
	}

	if len(repo.markArrivedCalls) != 1 || locator.asked[0] != "driver-1" {
		t.Fatalf("marked %v, asked %v", repo.markArrivedCalls, locator.asked)
	}
}

func TestArrivalRefusals(t *testing.T) {
	far := trip.DriverLocation{Latitude: 36.20, Longitude: 44.01} // about 1.1 km away

	cases := map[string]struct {
		trip     func(trip.Trip) trip.Trip
		position *trip.DriverLocation
		posErr   error
		want     error
	}{
		"only requested":   {trip: func(t trip.Trip) trip.Trip { t.Status = trip.StatusRequested; t.DriverID = ""; return t }, want: trip.ErrInvalidTransition},
		"already started":  {trip: func(t trip.Trip) trip.Trip { t.Status = trip.StatusInProgress; return t }, want: trip.ErrInvalidTransition},
		"position unknown": {posErr: trip.ErrDriverLocationUnavailable, want: trip.ErrArrivalPositionUnknown},
		"location-service": {posErr: errors.New("down"), want: nil},
		"too far":          {position: &far, want: trip.ErrTooFarFromPickup},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			current := acceptedTrip()
			if tc.trip != nil {
				current = tc.trip(current)
			}

			svc, repo, locator := arrivalRig(current)
			locator.err = tc.posErr

			if tc.position != nil {
				locator.position = *tc.position
			}

			_, err := svc.MarkDriverArrived(context.Background(), "trip-1")

			switch {
			case tc.want == nil && err == nil:
				t.Fatal("expected an error")
			case tc.want != nil && !errors.Is(err, tc.want):
				t.Fatalf("got %v, want %v", err, tc.want)
			}

			if len(repo.markArrivedCalls) != 0 {
				t.Fatal("nothing may be marked")
			}
		})
	}
}

func TestMarkingArrivalTwiceChangesNothing(t *testing.T) {
	current := acceptedTrip()
	earlier := time.Now().Add(-time.Minute)
	current.ArrivedAt = &earlier

	svc, repo, locator := arrivalRig(current)

	got, err := svc.MarkDriverArrived(context.Background(), "trip-1")
	if err != nil || !got.ArrivedAt.Equal(earlier) {
		t.Fatalf("got %+v %v", got, err)
	}

	if len(repo.markArrivedCalls) != 0 || len(locator.asked) != 0 {
		t.Fatal("an arrival already marked is not marked again")
	}
}

// --- cancelling -------------------------------------------------------------

func TestTheCancelRecordSaysWho(t *testing.T) {
	repo := &fakeRepository{cancelResult: trip.Trip{ID: "trip-1", Status: trip.StatusCancelled}}

	if _, err := newService(repo).CancelTrip(context.Background(), trip.CancelInput{TripID: "trip-1", By: trip.CancelledByDriver}); err != nil {
		t.Fatal(err)
	}

	if repo.cancelRecord.By != trip.CancelledByDriver || repo.cancelRecord.RiderNoShow {
		t.Fatalf("record %+v", repo.cancelRecord)
	}

	if _, err := newService(repo).CancelTrip(context.Background(), trip.CancelInput{TripID: "trip-1", By: "staff"}); !errors.Is(err, trip.ErrInvalidCancelledBy) {
		t.Fatalf("unknown canceller: %v", err)
	}
}

func TestANoShowIsTheDriversAfterWaiting(t *testing.T) {
	arrivedAgo := func(d time.Duration) *time.Time {
		at := time.Now().Add(-d)
		return &at
	}

	cases := map[string]struct {
		by      trip.CancelledBy
		current trip.Trip
		want    error
	}{
		"the rider cannot":     {trip.CancelledByRider, trip.Trip{Status: trip.StatusAccepted, ArrivedAt: arrivedAgo(10 * time.Minute)}, trip.ErrNoShowOnlyByDriver},
		"not arrived":          {trip.CancelledByDriver, trip.Trip{Status: trip.StatusAccepted}, trip.ErrNoShowTooEarly},
		"arrived a minute ago": {trip.CancelledByDriver, trip.Trip{Status: trip.StatusAccepted, ArrivedAt: arrivedAgo(time.Minute)}, trip.ErrNoShowTooEarly},
		"already started":      {trip.CancelledByDriver, trip.Trip{Status: trip.StatusInProgress, ArrivedAt: arrivedAgo(10 * time.Minute)}, trip.ErrNoShowTooEarly},
		"waited long enough":   {trip.CancelledByDriver, trip.Trip{Status: trip.StatusAccepted, ArrivedAt: arrivedAgo(6 * time.Minute)}, nil},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			repo := &fakeRepository{cancelCurrent: tc.current, cancelResult: trip.Trip{ID: "trip-1", Status: trip.StatusCancelled}}
			svc := trip.NewService(repo, &fakeIDGenerator{id: "x"}, &fakeZoneChecker{served: true}, trip.WithNoShowWait(5*time.Minute))

			_, err := svc.CancelTrip(context.Background(), trip.CancelInput{TripID: "trip-1", By: tc.by, RiderNoShow: true})
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}

			if tc.want == nil && !repo.cancelRecord.RiderNoShow {
				t.Fatalf("record %+v", repo.cancelRecord)
			}
		})
	}
}

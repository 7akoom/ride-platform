package trip

import (
	"context"
	"errors"
	"testing"
	"time"
)

type trackingFakeBase struct {
	Service

	trips map[string]Trip
	err   error
	reads int
}

func (b *trackingFakeBase) GetTrip(_ context.Context, tripID string) (Trip, error) {
	b.reads++

	if b.err != nil {
		return Trip{}, b.err
	}

	found, ok := b.trips[tripID]
	if !ok {
		return Trip{}, ErrTripNotFound
	}

	return found, nil
}

type trackingFakeLocator struct {
	location DriverLocation
	err      error
	asked    []string
}

func (l *trackingFakeLocator) DriverLocation(_ context.Context, driverID string) (DriverLocation, error) {
	l.asked = append(l.asked, driverID)

	return l.location, l.err
}

type driverTracker interface {
	GetDriverLocation(ctx context.Context, tripID string) (DriverLocation, error)
}

func newTrackingUnderTest(trips map[string]Trip) (driverTracker, *trackingFakeBase, *trackingFakeLocator) {
	base := &trackingFakeBase{trips: trips}
	locator := &trackingFakeLocator{location: DriverLocation{Latitude: 36.19, Longitude: 44.01, UpdatedAt: time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)}}

	tracker, ok := WithDriverTracking(base, locator).(driverTracker)
	if !ok {
		panic("the decorated service must expose GetDriverLocation")
	}

	return tracker, base, locator
}

func TestDriverLocationIsSharedWhileATripIsAcceptedOrInProgress(t *testing.T) {
	for _, status := range []Status{StatusAccepted, StatusInProgress} {
		tracker, _, locator := newTrackingUnderTest(map[string]Trip{
			"trip-1": {ID: "trip-1", RiderID: "rider-a", DriverID: "driver-a", Status: status},
		})

		got, err := tracker.GetDriverLocation(context.Background(), "trip-1")
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", status, err)
		}

		if got != locator.location {
			t.Errorf("%s: got %+v, want %+v", status, got, locator.location)
		}

		if len(locator.asked) != 1 || locator.asked[0] != "driver-a" {
			t.Errorf("%s: expected one lookup of driver-a, got %v", status, locator.asked)
		}
	}
}

func TestDriverLocationIsNotSharedOutsideAnActiveTrip(t *testing.T) {
	for _, status := range []Status{StatusRequested, StatusCompleted, StatusCancelled} {
		tracker, _, locator := newTrackingUnderTest(map[string]Trip{
			"trip-1": {ID: "trip-1", RiderID: "rider-a", DriverID: "driver-a", Status: status},
		})

		if _, err := tracker.GetDriverLocation(context.Background(), "trip-1"); !errors.Is(err, ErrTripNotTrackable) {
			t.Errorf("%s: expected ErrTripNotTrackable, got %v", status, err)
		}

		if len(locator.asked) != 0 {
			t.Errorf("%s: the driver was looked up %d time(s) for a trip that is not trackable", status, len(locator.asked))
		}
	}
}

func TestAnAcceptedTripWithoutADriverIsNotTrackable(t *testing.T) {
	tracker, _, locator := newTrackingUnderTest(map[string]Trip{
		"trip-1": {ID: "trip-1", RiderID: "rider-a", Status: StatusAccepted},
	})

	if _, err := tracker.GetDriverLocation(context.Background(), "trip-1"); !errors.Is(err, ErrTripNotTrackable) {
		t.Errorf("expected ErrTripNotTrackable, got %v", err)
	}

	if len(locator.asked) != 0 {
		t.Errorf("a driver id that does not exist was looked up: %v", locator.asked)
	}
}

func TestTripErrorsPassThrough(t *testing.T) {
	tracker, _, _ := newTrackingUnderTest(map[string]Trip{})

	if _, err := tracker.GetDriverLocation(context.Background(), "missing"); !errors.Is(err, ErrTripNotFound) {
		t.Errorf("expected ErrTripNotFound, got %v", err)
	}
}

func TestLocatorErrorsPassThrough(t *testing.T) {
	tracker, _, locator := newTrackingUnderTest(map[string]Trip{
		"trip-1": {ID: "trip-1", DriverID: "driver-a", Status: StatusAccepted},
	})

	locator.err = ErrDriverLocationUnavailable
	if _, err := tracker.GetDriverLocation(context.Background(), "trip-1"); !errors.Is(err, ErrDriverLocationUnavailable) {
		t.Errorf("expected ErrDriverLocationUnavailable, got %v", err)
	}

	boom := errors.New("location-service down")
	locator.err = boom
	if _, err := tracker.GetDriverLocation(context.Background(), "trip-1"); !errors.Is(err, boom) {
		t.Errorf("expected the locator's error, got %v", err)
	}
}

func TestTheDecoratedServiceStillServesEveryOtherMethod(t *testing.T) {
	base := &trackingFakeBase{trips: map[string]Trip{"trip-1": {ID: "trip-1"}}}
	decorated := WithDriverTracking(base, &trackingFakeLocator{})

	got, err := decorated.GetTrip(context.Background(), "trip-1")
	if err != nil || got.ID != "trip-1" || base.reads != 1 {
		t.Errorf("GetTrip must reach the base service: %+v, %v, reads=%d", got, err, base.reads)
	}
}

func TestWithDriverTrackingRequiresBothParts(t *testing.T) {
	for name, build := range map[string]func(){
		"no base service": func() { WithDriverTracking(nil, &trackingFakeLocator{}) },
		"no locator":      func() { WithDriverTracking(&trackingFakeBase{}, nil) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: expected a panic", name)
				}
			}()

			build()
		}()
	}
}

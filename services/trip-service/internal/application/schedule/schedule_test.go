package schedule

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

const (
	riderID   = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	addressID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	rideID    = "11111111-1111-4111-8111-111111111111"
)

var testNow = time.Date(2026, 9, 25, 9, 0, 0, 0, time.UTC)

var testLimits = Limits{MinAhead: 30 * time.Minute, MaxAhead: 7 * 24 * time.Hour, MaxUpcoming: 3, DispatchLead: 10 * time.Minute, Grace: 10 * time.Minute}

type fakeStore struct {
	Store

	byKey      map[string]Ride
	created    []Ride
	dispatched map[string]string
	notSched   bool
	retried    []string
	failed     []string
}

func newFakeStore() *fakeStore {
	return &fakeStore{byKey: map[string]Ride{}, dispatched: map[string]string{}}
}

func (f *fakeStore) Create(_ context.Context, r Ride, _ int) (Ride, bool, error) {
	f.created = append(f.created, r)
	f.byKey[r.RiderID+"/"+r.IdempotencyKey] = r

	return r, false, nil
}

func (f *fakeStore) FindByKey(_ context.Context, rider, key string) (Ride, bool, error) {
	r, ok := f.byKey[rider+"/"+key]

	return r, ok, nil
}

func (f *fakeStore) MarkDispatched(_ context.Context, id, tripID string, _ time.Time) error {
	if f.notSched {
		return ErrNotScheduled
	}

	f.dispatched[id] = tripID

	return nil
}

func (f *fakeStore) Retry(_ context.Context, _ string, reason string, _ time.Time) error {
	f.retried = append(f.retried, reason)

	return nil
}

func (f *fakeStore) Fail(_ context.Context, _ string, reason string, _ time.Time) error {
	f.failed = append(f.failed, reason)

	return nil
}

type fakeZones struct {
	served bool
	zone   string
}

func (z fakeZones) Locate(context.Context, float64, float64) (bool, string, error) {
	return z.served, z.zone, nil
}

type fakeBook struct{}

func (fakeBook) SavedAddress(_ context.Context, _, id string) (trip.SavedAddress, error) {
	if id != addressID {
		return trip.SavedAddress{}, trip.ErrSavedAddressNotFound
	}

	return trip.SavedAddress{Coordinates: trip.Coordinates{Latitude: 36.19, Longitude: 44.01}, Address: "Home"}, nil
}

type fixedID string

func (f fixedID) NewID() string { return string(f) }

func newTestService(store *fakeStore) *Service {
	s := NewService(store, fakeZones{served: true, zone: "Asia/Baghdad"}, fakeBook{}, fixedID(rideID), testLimits)
	s.now = func() time.Time { return testNow }

	return s
}

func validBooking() BookInput {
	return BookInput{
		RiderID: riderID, PickupLat: 36.1, PickupLng: 44.1, DropoffLat: 36.2, DropoffLng: 44.2,
		ScheduledAt: testNow.Add(2 * time.Hour), IdempotencyKey: "k1",
	}
}

func TestABookingIsCheckedAndMadeOnce(t *testing.T) {
	for name, c := range map[string]struct {
		edit func(*BookInput)
		want error
	}{
		"no rider":             {func(in *BookInput) { in.RiderID = "" }, ErrRiderRequired},
		"no key":               {func(in *BookInput) { in.IdempotencyKey = "" }, ErrIdempotencyKey},
		"in twenty minutes":    {func(in *BookInput) { in.ScheduledAt = testNow.Add(20 * time.Minute) }, ErrTooSoon},
		"in eight days":        {func(in *BookInput) { in.ScheduledAt = testNow.Add(8 * 24 * time.Hour) }, ErrTooFar},
		"a bad class":          {func(in *BookInput) { in.VehicleClass = "helicopter" }, trip.ErrInvalidVehicleClass},
		"a passenger half":     {func(in *BookInput) { in.PassengerName = "Sara" }, trip.ErrInvalidPassenger},
		"a bad latitude":       {func(in *BookInput) { in.PickupLat = 91 }, trip.ErrInvalidLatitude},
		"a saved address gone": {func(in *BookInput) { in.PickupSavedAddressID = "cccccccc-cccc-4ccc-8ccc-cccccccccccc" }, trip.ErrSavedAddressNotFound},
		"not a saved address":  {func(in *BookInput) { in.DropoffSavedAddressID = "home" }, trip.ErrSavedAddressNotFound},
	} {
		input := validBooking()
		c.edit(&input)

		if _, err := newTestService(newFakeStore()).Book(context.Background(), input); !errors.Is(err, c.want) {
			t.Errorf("%s: got %v, want %v", name, err, c.want)
		}
	}

	store := newFakeStore()
	s := newTestService(store)

	input := validBooking()
	input.PickupSavedAddressID = addressID
	input.PassengerName, input.PassengerPhone = " Sara ", "+9647500000002"

	booked, err := s.Book(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}

	if booked.ID != rideID || booked.PickupAddress != "Home" || booked.Pickup.Latitude != 36.19 || booked.PickupSavedAddressID != addressID ||
		booked.TimeZone != "Asia/Baghdad" || booked.PassengerName != "Sara" || booked.VehicleClass != "economy" || booked.PaymentMethod != "cash" ||
		!booked.NextAttemptAt.Equal(input.ScheduledAt.Add(-10*time.Minute)) {
		t.Fatalf("booked %+v", booked)
	}

	if booked.Local() != "2026-09-25 14:00" {
		t.Fatalf("local %q", booked.Local())
	}

	if again, err := s.Book(context.Background(), input); err != nil || again.ID != rideID || len(store.created) != 1 {
		t.Fatalf("again %+v %v", again, err)
	}

	later := input
	later.ScheduledAt = input.ScheduledAt.Add(time.Hour)

	if _, err := s.Book(context.Background(), later); !errors.Is(err, ErrKeyReused) {
		t.Fatalf("the key for another time: %v", err)
	}
}

func TestABookingOutsideEveryZoneIsRefused(t *testing.T) {
	s := NewService(newFakeStore(), fakeZones{served: false}, fakeBook{}, fixedID(rideID), testLimits)
	s.now = func() time.Time { return testNow }

	if _, err := s.Book(context.Background(), validBooking()); !errors.Is(err, trip.ErrPickupOutsideServiceZone) {
		t.Fatalf("got %v", err)
	}
}

type fakeTrips struct {
	existing  *trip.Trip
	requests  []trip.RequestTripInput
	errs      []error
	cancelled []trip.CancelInput
}

func (f *fakeTrips) GetTrip(context.Context, string) (trip.Trip, error) {
	if f.existing != nil {
		return *f.existing, nil
	}

	return trip.Trip{}, trip.ErrTripNotFound
}

func (f *fakeTrips) RequestTrip(_ context.Context, in trip.RequestTripInput) (trip.Trip, error) {
	f.requests = append(f.requests, in)

	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]

		if err != nil {
			return trip.Trip{}, err
		}
	}

	return trip.Trip{ID: in.ScheduledTripID}, nil
}

func (f *fakeTrips) CancelTrip(_ context.Context, in trip.CancelInput) (trip.Trip, error) {
	f.cancelled = append(f.cancelled, in)

	return trip.Trip{}, nil
}

func dueRide() Ride {
	return Ride{
		ID: rideID, RiderID: riderID, ScheduledAt: testNow.Add(10 * time.Minute),
		PickupSavedAddressID: addressID, PassengerName: "Sara", PassengerPhone: "+9647500000002",
	}
}

func newTestDispatcher(store *fakeStore, trips *fakeTrips) *Dispatcher {
	d := NewDispatcher(store, trips, testLimits, time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
	d.now = func() time.Time { return testNow }

	return d
}

func TestADueBookingBecomesATripWithItsID(t *testing.T) {
	store, trips := newFakeStore(), &fakeTrips{}

	if err := newTestDispatcher(store, trips).dispatch(context.Background(), dueRide(), testNow); err != nil {
		t.Fatal(err)
	}

	sent := trips.requests[0]
	if sent.ScheduledTripID != rideID || sent.PickupSavedAddressID != addressID || sent.PassengerName != "Sara" || store.dispatched[rideID] != rideID {
		t.Fatalf("sent %+v, dispatched %+v", sent, store.dispatched)
	}
}

func TestATripMadeInAnEarlierRoundIsRecordedNotMadeAgain(t *testing.T) {
	store, trips := newFakeStore(), &fakeTrips{existing: &trip.Trip{ID: rideID}}

	if err := newTestDispatcher(store, trips).dispatch(context.Background(), dueRide(), testNow); err != nil {
		t.Fatal(err)
	}

	if len(trips.requests) != 0 || store.dispatched[rideID] != rideID {
		t.Fatalf("requests %d, dispatched %+v", len(trips.requests), store.dispatched)
	}
}

func TestATripOfABookingCancelledMeanwhileIsCancelled(t *testing.T) {
	store, trips := newFakeStore(), &fakeTrips{}
	store.notSched = true

	if err := newTestDispatcher(store, trips).dispatch(context.Background(), dueRide(), testNow); err != nil {
		t.Fatal(err)
	}

	if len(trips.cancelled) != 1 || trips.cancelled[0].TripID != rideID || trips.cancelled[0].By != trip.CancelledBySystem {
		t.Fatalf("cancelled %+v", trips.cancelled)
	}
}

func TestADeletedSavedAddressFallsBackToTheBookedPoint(t *testing.T) {
	store, trips := newFakeStore(), &fakeTrips{errs: []error{trip.ErrSavedAddressNotFound, nil}}

	if err := newTestDispatcher(store, trips).dispatch(context.Background(), dueRide(), testNow); err != nil {
		t.Fatal(err)
	}

	if len(trips.requests) != 2 || trips.requests[1].PickupSavedAddressID != "" || store.dispatched[rideID] == "" {
		t.Fatalf("requests %+v", trips.requests)
	}
}

func TestABookingThatCannotBeDispatchedIsTriedUntilItsGraceEnds(t *testing.T) {
	store, trips := newFakeStore(), &fakeTrips{errs: []error{trip.ErrRiderHasActiveTrip, errors.New("location-service down")}}
	d := newTestDispatcher(store, trips)

	// Before the time: tried again, with the reason the rider can read.
	if err := d.dispatch(context.Background(), dueRide(), testNow); err != nil {
		t.Fatal(err)
	}

	if len(store.retried) != 1 || store.retried[0] != trip.ErrRiderHasActiveTrip.Error() || len(store.failed) != 0 {
		t.Fatalf("retried %+v failed %+v", store.retried, store.failed)
	}

	// Past the time and its grace: it fails, and an internal error is told plainly.
	late := testNow.Add(25 * time.Minute)
	if err := d.dispatch(context.Background(), dueRide(), late); err != nil {
		t.Fatal(err)
	}

	if len(store.failed) != 1 || store.failed[0] != "the trip could not be requested" {
		t.Fatalf("failed %+v", store.failed)
	}
}

func TestABookingKeepsItsStopsAndItsTripGetsThem(t *testing.T) {
	store := newFakeStore()
	s := newTestService(store)

	input := validBooking()
	input.Stops = []trip.Stop{{Coordinates: trip.Coordinates{Latitude: 36.15, Longitude: 44.15}, Address: " Bakery "}}

	booked, err := s.Book(context.Background(), input)
	if err != nil || len(booked.Stops) != 1 || booked.Stops[0].Address != "Bakery" {
		t.Fatalf("booked %+v %v", booked.Stops, err)
	}

	// The same key without the stop is another booking.
	again := input
	again.Stops = nil

	if _, err := s.Book(context.Background(), again); !errors.Is(err, ErrKeyReused) {
		t.Fatalf("the key without the stop: %v", err)
	}

	tooMany := validBooking()
	tooMany.IdempotencyKey = "k2"
	tooMany.Stops = make([]trip.Stop, 3)

	if _, err := s.Book(context.Background(), tooMany); !errors.Is(err, trip.ErrTooManyStops) {
		t.Fatalf("three stops: %v", err)
	}

	trips := &fakeTrips{}
	ride := dueRide()
	ride.Stops = booked.Stops

	if err := newTestDispatcher(newFakeStore(), trips).dispatch(context.Background(), ride, testNow); err != nil {
		t.Fatal(err)
	}

	if len(trips.requests[0].Stops) != 1 || trips.requests[0].Stops[0].Address != "Bakery" {
		t.Fatalf("requested with %+v", trips.requests[0].Stops)
	}
}

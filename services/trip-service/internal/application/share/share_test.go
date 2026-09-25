package share

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

var testNow = time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

type fakeStore struct {
	links   []Link
	revoked int
}

func (f *fakeStore) Create(_ context.Context, link Link, maxLive int) (Link, error) {
	live := 0
	for _, l := range f.links {
		if l.TripID == link.TripID && l.RevokedAt == nil && l.ExpiresAt.After(link.CreatedAt) {
			live++
		}
	}

	if live >= maxLive {
		return Link{}, ErrTooManyLinks
	}

	link.ID = "link"
	f.links = append(f.links, link)

	return link, nil
}

func (f *fakeStore) FindLive(_ context.Context, hash []byte, now time.Time) (Link, error) {
	for _, l := range f.links {
		if bytes.Equal(l.TokenHash, hash) && l.RevokedAt == nil && l.ExpiresAt.After(now) {
			return l, nil
		}
	}

	return Link{}, ErrNotFound
}

func (f *fakeStore) RevokeAll(_ context.Context, tripID string, now time.Time) (int, error) {
	n := 0
	for i := range f.links {
		if f.links[i].TripID == tripID && f.links[i].RevokedAt == nil {
			f.links[i].RevokedAt = &now
			n++
		}
	}

	f.revoked += n

	return n, nil
}

type fakeTrips struct{ trip trip.Trip }

func (f *fakeTrips) GetTrip(_ context.Context, id string) (trip.Trip, error) {
	if id != f.trip.ID {
		return trip.Trip{}, trip.ErrTripNotFound
	}

	return f.trip, nil
}

type fakeDrivers struct{ err error }

func (f fakeDrivers) DriverSummary(context.Context, string) (trip.DriverSummary, error) {
	return trip.DriverSummary{DisplayName: "Karwan", PlateNumber: "12345"}, f.err
}

type fakeLocator struct{ err error }

func (f fakeLocator) DriverLocation(context.Context, string) (trip.DriverLocation, error) {
	return trip.DriverLocation{Latitude: 36.2, Longitude: 44.0}, f.err
}

func rig(t trip.Trip) (*Service, *fakeStore, *fakeTrips) {
	store, trips := &fakeStore{}, &fakeTrips{trip: t}
	s := NewService(store, trips, fakeDrivers{}, fakeLocator{}, Settings{URLBase: "https://ride.example/t/", MaxAge: 12 * time.Hour, AfterEnd: 30 * time.Minute})
	s.now = func() time.Time { return testNow }

	return s, store, trips
}

func tripUnderWay() trip.Trip {
	return trip.Trip{ID: "trip-1", RiderID: "rider-1", DriverID: "driver-1", Status: trip.StatusInProgress}
}

func TestALinkIsATokenOnlyItsHashIsKept(t *testing.T) {
	s, store, _ := rig(tripUnderWay())

	made, err := s.Create(context.Background(), "trip-1")
	if err != nil {
		t.Fatal(err)
	}

	if !wellFormed(made.Token) || made.URL != "https://ride.example/t/"+made.Token || !made.ExpiresAt.Equal(testNow.Add(12*time.Hour)) {
		t.Fatalf("made %+v", made)
	}

	if len(store.links) != 1 || bytes.Contains(store.links[0].TokenHash, []byte(made.Token)) || len(store.links[0].TokenHash) != 32 {
		t.Fatalf("stored %+v", store.links)
	}

	again, _ := s.Create(context.Background(), "trip-1")
	if again.Token == made.Token {
		t.Fatal("two links, one token")
	}
}

func TestWithoutABaseTheURLIsLeftToTheApp(t *testing.T) {
	s, _, _ := rig(tripUnderWay())
	s.settings.URLBase = ""

	if made, err := s.Create(context.Background(), "trip-1"); err != nil || made.URL != "" || made.Token == "" {
		t.Fatalf("made %+v %v", made, err)
	}
}

func TestOnlyATripUnderWayIsSharedAndOnlySoOften(t *testing.T) {
	done := tripUnderWay()
	done.Status = trip.StatusCompleted

	s, _, _ := rig(done)
	if _, err := s.Create(context.Background(), "trip-1"); !errors.Is(err, ErrTripNotLive) {
		t.Fatalf("a completed trip: %v", err)
	}

	s, _, _ = rig(tripUnderWay())
	for i := 0; i < MaxLiveLinks; i++ {
		if _, err := s.Create(context.Background(), "trip-1"); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := s.Create(context.Background(), "trip-1"); !errors.Is(err, ErrTooManyLinks) {
		t.Fatalf("one more: %v", err)
	}

	if n, _ := s.Stop(context.Background(), "trip-1"); n != MaxLiveLinks {
		t.Fatalf("stopped %d", n)
	}

	if _, err := s.Create(context.Background(), "trip-1"); err != nil {
		t.Fatalf("after stopping: %v", err)
	}
}

func TestALinkShowsTheTripItsDriverAndWhereTheyAre(t *testing.T) {
	s, _, _ := rig(tripUnderWay())
	made, _ := s.Create(context.Background(), "trip-1")

	view, err := s.View(context.Background(), made.Token)
	if err != nil || view.Trip.ID != "trip-1" || view.Driver == nil || view.Driver.DisplayName != "Karwan" ||
		view.Location == nil || !view.ExpiresAt.Equal(made.ExpiresAt) {
		t.Fatalf("view %+v %v", view, err)
	}

	// Who drives and where are extras: without them, the trip still shows.
	s.drivers, s.locator = fakeDrivers{err: errors.New("down")}, fakeLocator{err: trip.ErrDriverLocationUnavailable}

	if view, err := s.View(context.Background(), made.Token); err != nil || view.Driver != nil || view.Location != nil {
		t.Fatalf("without extras %+v %v", view, err)
	}
}

func TestNoPositionOnceTheTripIsOver(t *testing.T) {
	s, _, trips := rig(tripUnderWay())
	made, _ := s.Create(context.Background(), "trip-1")

	ended := testNow.Add(-10 * time.Minute)
	trips.trip.Status, trips.trip.CompletedAt = trip.StatusCompleted, &ended

	view, err := s.View(context.Background(), made.Token)
	if err != nil || view.Location != nil || view.Driver == nil || !view.ExpiresAt.Equal(ended.Add(30*time.Minute)) {
		t.Fatalf("view %+v %v", view, err)
	}
}

func TestALinkStopsWorking(t *testing.T) {
	cases := map[string]func(s *Service, store *fakeStore, trips *fakeTrips, token string) string{
		"a made-up token": func(*Service, *fakeStore, *fakeTrips, string) string {
			return strings.Repeat("a", 43)
		},
		"a malformed one": func(*Service, *fakeStore, *fakeTrips, string) string { return "short" },
		"stopped": func(s *Service, _ *fakeStore, _ *fakeTrips, token string) string {
			_, _ = s.Stop(context.Background(), "trip-1")
			return token
		},
		"expired": func(s *Service, _ *fakeStore, _ *fakeTrips, token string) string {
			s.now = func() time.Time { return testNow.Add(13 * time.Hour) }
			return token
		},
		"the trip ended long ago": func(_ *Service, _ *fakeStore, trips *fakeTrips, token string) string {
			ended := testNow.Add(-31 * time.Minute)
			trips.trip.Status, trips.trip.CancelledAt = trip.StatusCancelled, &ended
			return token
		},
	}

	for name, edit := range cases {
		s, store, trips := rig(tripUnderWay())
		made, _ := s.Create(context.Background(), "trip-1")

		if _, err := s.View(context.Background(), edit(s, store, trips, made.Token)); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

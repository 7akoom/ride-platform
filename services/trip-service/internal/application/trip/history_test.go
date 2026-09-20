package trip

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"testing"
)

func historyTripID(n int) string {
	return fmt.Sprintf("00000000-0000-4000-8000-%012d", n)
}

// historyFakeBase satisfies Service for the decorator; nothing else is called.
type historyFakeBase struct{ Service }

type historyFakeStore struct {
	// trips are the owner's trips, newest first.
	trips []Trip

	active    Trip
	activeErr error
	listErr   error

	calls []historyCall
}

type historyCall struct {
	role  string
	owner string
	after string
	limit int
}

func (s *historyFakeStore) FindActiveByRiderID(_ context.Context, riderID string) (Trip, error) {
	s.calls = append(s.calls, historyCall{role: "rider", owner: riderID})

	return s.active, s.activeErr
}

func (s *historyFakeStore) FindActiveByDriverID(_ context.Context, driverID string) (Trip, error) {
	s.calls = append(s.calls, historyCall{role: "driver", owner: driverID})

	return s.active, s.activeErr
}

func (s *historyFakeStore) list(role, owner, after string, limit int) ([]Trip, error) {
	s.calls = append(s.calls, historyCall{role: role, owner: owner, after: after, limit: limit})

	if s.listErr != nil {
		return nil, s.listErr
	}

	start := 0

	if after != "" {
		for i, t := range s.trips {
			if t.ID == after {
				start = i + 1
			}
		}
	}

	end := start + limit
	if end > len(s.trips) {
		end = len(s.trips)
	}

	return append([]Trip(nil), s.trips[start:end]...), nil
}

func (s *historyFakeStore) ListByRiderID(_ context.Context, riderID string, after string, limit int) ([]Trip, error) {
	return s.list("rider", riderID, after, limit)
}

func (s *historyFakeStore) ListByDriverID(_ context.Context, driverID string, after string, limit int) ([]Trip, error) {
	return s.list("driver", driverID, after, limit)
}

type historyReader interface {
	GetActiveTrip(ctx context.Context, riderID string, driverID string) (Trip, error)
	ListTrips(ctx context.Context, query HistoryQuery) (HistoryPage, error)
}

func newHistoryUnderTest(t *testing.T, trips int) (historyReader, *historyFakeStore) {
	t.Helper()

	store := &historyFakeStore{}
	for n := trips; n >= 1; n-- { // newest (highest number) first
		store.trips = append(store.trips, Trip{ID: historyTripID(n), RiderID: "rider-a"})
	}

	reader, ok := As[historyReader](WithTripHistory(&historyFakeBase{}, store))
	if !ok {
		t.Fatal("the decorated service must expose the history methods")
	}

	return reader, store
}

func TestGetActiveTripAsksTheStoreForTheRoleGiven(t *testing.T) {
	reader, store := newHistoryUnderTest(t, 0)
	store.active = Trip{ID: historyTripID(1), Status: StatusAccepted}

	if got, err := reader.GetActiveTrip(context.Background(), "rider-a", ""); err != nil || got.ID != store.active.ID {
		t.Fatalf("rider: got %+v, %v", got, err)
	}

	if got, err := reader.GetActiveTrip(context.Background(), "", "driver-a"); err != nil || got.ID != store.active.ID {
		t.Fatalf("driver: got %+v, %v", got, err)
	}

	if len(store.calls) != 2 || store.calls[0] != (historyCall{role: "rider", owner: "rider-a"}) || store.calls[1] != (historyCall{role: "driver", owner: "driver-a"}) {
		t.Errorf("unexpected store calls: %+v", store.calls)
	}
}

func TestGetActiveTripNeedsExactlyOneOwner(t *testing.T) {
	reader, store := newHistoryUnderTest(t, 0)

	for name, ids := range map[string][2]string{"neither": {"", ""}, "both": {"rider-a", "driver-a"}} {
		if _, err := reader.GetActiveTrip(context.Background(), ids[0], ids[1]); !errors.Is(err, ErrInvalidHistoryQuery) {
			t.Errorf("%s: expected ErrInvalidHistoryQuery, got %v", name, err)
		}
	}

	if len(store.calls) != 0 {
		t.Errorf("the store must not be asked about an invalid query: %+v", store.calls)
	}
}

func TestGetActiveTripPassesTheStoresAnswerThrough(t *testing.T) {
	reader, store := newHistoryUnderTest(t, 0)

	store.activeErr = ErrTripNotFound
	if _, err := reader.GetActiveTrip(context.Background(), "rider-a", ""); !errors.Is(err, ErrTripNotFound) {
		t.Errorf("expected ErrTripNotFound, got %v", err)
	}

	boom := errors.New("database down")
	store.activeErr = boom
	if _, err := reader.GetActiveTrip(context.Background(), "", "driver-a"); !errors.Is(err, boom) {
		t.Errorf("expected the store's error, got %v", err)
	}
}

func TestListTripsPagesThroughAllTripsNewestFirstWithoutGapsOrRepeats(t *testing.T) {
	reader, _ := newHistoryUnderTest(t, 7)

	var seen []string

	token := ""

	for pages := 0; ; pages++ {
		if pages > 10 {
			t.Fatal("paging did not end")
		}

		page, err := reader.ListTrips(context.Background(), HistoryQuery{RiderID: "rider-a", PageSize: 3, PageToken: token})
		if err != nil {
			t.Fatalf("page %d: %v", pages+1, err)
		}

		if len(page.Trips) > 3 {
			t.Fatalf("page %d holds %d trips, more than the page size", pages+1, len(page.Trips))
		}

		for _, trip := range page.Trips {
			seen = append(seen, trip.ID)
		}

		if page.NextPageToken == "" {
			break
		}

		token = page.NextPageToken
	}

	if len(seen) != 7 {
		t.Fatalf("expected 7 trips over the pages, got %d: %v", len(seen), seen)
	}

	for i, id := range seen {
		if want := historyTripID(7 - i); id != want {
			t.Errorf("position %d: got %s, want %s", i, id, want)
		}
	}
}

func TestTheLastPageHasNoNextToken(t *testing.T) {
	reader, _ := newHistoryUnderTest(t, 3)

	page, err := reader.ListTrips(context.Background(), HistoryQuery{RiderID: "rider-a", PageSize: 3})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(page.Trips) != 3 || page.NextPageToken != "" {
		t.Errorf("a page that exactly ends the history must not promise another: %d trips, token %q", len(page.Trips), page.NextPageToken)
	}
}

func TestAnEmptyHistoryIsAnEmptyPage(t *testing.T) {
	reader, _ := newHistoryUnderTest(t, 0)

	page, err := reader.ListTrips(context.Background(), HistoryQuery{DriverID: "driver-a"})
	if err != nil || len(page.Trips) != 0 || page.NextPageToken != "" {
		t.Errorf("got %+v, %v", page, err)
	}
}

func TestPageSizes(t *testing.T) {
	cases := []struct {
		asked int
		want  int // the limit the store is asked for, which is one more than the page
	}{
		{0, DefaultHistoryPageSize + 1},
		{1, 2},
		{MaxHistoryPageSize, MaxHistoryPageSize + 1},
		{MaxHistoryPageSize + 100, MaxHistoryPageSize + 1},
	}

	for _, tc := range cases {
		reader, store := newHistoryUnderTest(t, 0)

		if _, err := reader.ListTrips(context.Background(), HistoryQuery{RiderID: "rider-a", PageSize: tc.asked}); err != nil {
			t.Fatalf("page size %d: %v", tc.asked, err)
		}

		if got := store.calls[0].limit; got != tc.want {
			t.Errorf("page size %d: the store was asked for %d, expected %d", tc.asked, got, tc.want)
		}
	}

	reader, _ := newHistoryUnderTest(t, 0)
	if _, err := reader.ListTrips(context.Background(), HistoryQuery{RiderID: "rider-a", PageSize: -1}); !errors.Is(err, ErrInvalidPageSize) {
		t.Errorf("a negative page size: expected ErrInvalidPageSize, got %v", err)
	}
}

func TestListTripsAsksTheRightStoreForTheRole(t *testing.T) {
	reader, store := newHistoryUnderTest(t, 1)

	_, _ = reader.ListTrips(context.Background(), HistoryQuery{RiderID: "rider-a"})
	_, _ = reader.ListTrips(context.Background(), HistoryQuery{DriverID: "driver-a"})

	if len(store.calls) != 2 || store.calls[0].role != "rider" || store.calls[0].owner != "rider-a" || store.calls[1].role != "driver" || store.calls[1].owner != "driver-a" {
		t.Errorf("unexpected store calls: %+v", store.calls)
	}
}

func TestListTripsNeedsExactlyOneOwner(t *testing.T) {
	reader, store := newHistoryUnderTest(t, 1)

	for name, query := range map[string]HistoryQuery{
		"neither": {},
		"both":    {RiderID: "rider-a", DriverID: "driver-a"},
	} {
		if _, err := reader.ListTrips(context.Background(), query); !errors.Is(err, ErrInvalidHistoryQuery) {
			t.Errorf("%s: expected ErrInvalidHistoryQuery, got %v", name, err)
		}
	}

	if len(store.calls) != 0 {
		t.Errorf("the store must not be asked about an invalid query: %+v", store.calls)
	}
}

func TestPageTokensAreValidatedBeforeTheStoreIsAsked(t *testing.T) {
	forged := func(payload string) string { return encodeRawToken(payload) }

	for name, token := range map[string]string{
		"not base64":                  "%%%not-base64%%%",
		"base64 of something else":    forged("hello"),
		"the right prefix, not an id": forged("t1:1; DROP TABLE trips"),
		"an id with a wrong length":   forged("t1:" + historyTripID(1) + "0"),
		"a prefix from another kind":  forged("t2:" + historyTripID(1)),
		"the right length, not hex":   forged("t1:zzzzzzzz-zzzz-zzzz-zzzz-zzzzzzzzzzzz"),
		"the right length, no dashes": forged("t1:000000000000400080000000000000000001"),
	} {
		reader, store := newHistoryUnderTest(t, 3)

		if _, err := reader.ListTrips(context.Background(), HistoryQuery{RiderID: "rider-a", PageToken: token}); !errors.Is(err, ErrInvalidPageToken) {
			t.Errorf("%s: expected ErrInvalidPageToken, got %v", name, err)
		}

		if len(store.calls) != 0 {
			t.Errorf("%s: the store was asked with a bad token: %+v", name, store.calls)
		}
	}
}

func encodeRawToken(payload string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

func TestAPageTokenRoundTrips(t *testing.T) {
	id := historyTripID(42)

	got, err := decodePageToken(encodePageToken(id))
	if err != nil || got != id {
		t.Errorf("got %q, %v", got, err)
	}
}

func TestListTripsWrapsTheStoresError(t *testing.T) {
	reader, store := newHistoryUnderTest(t, 3)
	boom := errors.New("database down")
	store.listErr = boom

	if _, err := reader.ListTrips(context.Background(), HistoryQuery{RiderID: "rider-a"}); !errors.Is(err, boom) {
		t.Errorf("expected the store's error, got %v", err)
	}
}

func TestTheHistoryDecoratorServesEveryOtherMethodFromTheBase(t *testing.T) {
	base := &trackingFakeBase{trips: map[string]Trip{"trip-1": {ID: "trip-1"}}}
	decorated := WithTripHistory(base, &historyFakeStore{})

	if got, err := decorated.GetTrip(context.Background(), "trip-1"); err != nil || got.ID != "trip-1" || base.reads != 1 {
		t.Errorf("GetTrip must reach the base service: %+v, %v, reads=%d", got, err, base.reads)
	}
}

func TestTheHistoryDecoratorRequiresBothParts(t *testing.T) {
	for name, build := range map[string]func(){
		"no base service": func() { WithTripHistory(nil, &historyFakeStore{}) },
		"no store":        func() { WithTripHistory(&historyFakeBase{}, nil) },
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

func TestHistoryAndTrackingCanBeStackedInEitherOrder(t *testing.T) {
	type tracker interface {
		GetDriverLocation(ctx context.Context, tripID string) (DriverLocation, error)
	}

	base := &trackingFakeBase{}

	orders := map[string]Service{
		"history outside": WithTripHistory(WithDriverTracking(base, &trackingFakeLocator{}), &historyFakeStore{}),
		"history inside":  WithDriverTracking(WithTripHistory(base, &historyFakeStore{}), &trackingFakeLocator{}),
	}

	for name, stack := range orders {
		if _, ok := As[tracker](stack); !ok {
			t.Errorf("%s: driver tracking is not reachable", name)
		}

		if _, ok := As[historyReader](stack); !ok {
			t.Errorf("%s: the history is not reachable", name)
		}
	}
}

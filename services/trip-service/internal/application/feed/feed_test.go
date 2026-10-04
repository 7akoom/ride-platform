package feed

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

var base = time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)

type memTrips struct{ trips []trip.Trip }

func (m *memTrips) ListTripsBefore(_ context.Context, _ Owner, _ string, before *Cursor, limit int) ([]trip.Trip, error) {
	all := append([]trip.Trip(nil), m.trips...)
	sort.Slice(all, func(i, j int) bool {
		if !all[i].RequestedAt.Equal(all[j].RequestedAt) {
			return all[i].RequestedAt.After(all[j].RequestedAt)
		}
		return all[i].ID > all[j].ID
	})

	var out []trip.Trip
	for _, t := range all {
		if before == nil || before.after(t.RequestedAt, t.ID) {
			out = append(out, t)
		}
	}

	if len(out) > limit {
		out = out[:limit]
	}

	return out, nil
}

type memWallet struct {
	movements []Movement
	err       error
}

func (m *memWallet) MovementsBefore(_ context.Context, _ Owner, _ string, before *Cursor, limit int) ([]Movement, error) {
	if m.err != nil {
		return nil, m.err
	}

	all := append([]Movement(nil), m.movements...)
	sort.Slice(all, func(i, j int) bool {
		if !all[i].At.Equal(all[j].At) {
			return all[i].At.After(all[j].At)
		}
		return all[i].ID > all[j].ID
	})

	var out []Movement
	for _, mv := range all {
		if before == nil || before.after(mv.At, mv.ID) {
			out = append(out, mv)
		}
	}

	if len(out) > limit {
		out = out[:limit]
	}

	return out, nil
}

func TestPagesMergeBothSourcesWithoutGapsOrRepeats(t *testing.T) {
	trips := &memTrips{}
	wallet := &memWallet{}

	// 7 trips and 6 movements, two of them at the same instant as a trip.
	for i := 0; i < 7; i++ {
		trips.trips = append(trips.trips, trip.Trip{ID: fmt.Sprintf("t%02d", i), RequestedAt: base.Add(time.Duration(i*2) * time.Minute)})
	}

	for i := 0; i < 6; i++ {
		wallet.movements = append(wallet.movements, Movement{ID: fmt.Sprintf("w%02d", i), At: base.Add(time.Duration(i*2+1) * time.Minute)})
	}

	wallet.movements = append(wallet.movements, Movement{ID: "w99", At: base.Add(4 * time.Minute)})

	s := NewService(trips, wallet)
	seen := map[string]bool{}
	var order []time.Time
	token := ""

	for pages := 0; pages < 10; pages++ {
		page, err := s.List(context.Background(), "r1", "", 3, token)
		if err != nil {
			t.Fatal(err)
		}

		for _, it := range page.Items {
			if seen[it.ID] {
				t.Fatalf("%s twice", it.ID)
			}

			seen[it.ID] = true
			order = append(order, it.At)
		}

		if page.NextPageToken == "" {
			break
		}

		token = page.NextPageToken
	}

	if len(seen) != 14 {
		t.Fatalf("saw %d of 14", len(seen))
	}

	for i := 1; i < len(order); i++ {
		if order[i].After(order[i-1]) {
			t.Fatalf("not newest first at %d", i)
		}
	}
}

func TestListRefusesBadQueries(t *testing.T) {
	s := NewService(&memTrips{}, &memWallet{})

	if _, err := s.List(context.Background(), "r", "d", 10, ""); !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("both: %v", err)
	}

	if _, err := s.List(context.Background(), "", "", 10, ""); !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("neither: %v", err)
	}

	if _, err := s.List(context.Background(), "r", "", 10, "!!"); !errors.Is(err, ErrInvalidPageToken) {
		t.Fatalf("bad token: %v", err)
	}

	broken := NewService(&memTrips{}, &memWallet{err: fmt.Errorf("%w: down", ErrWalletUnavailable)})
	if _, err := broken.List(context.Background(), "r", "", 10, ""); !errors.Is(err, ErrWalletUnavailable) {
		t.Fatalf("wallet down: %v", err)
	}
}

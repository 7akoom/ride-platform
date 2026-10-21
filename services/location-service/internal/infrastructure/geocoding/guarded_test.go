package geocoding

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/location-service/internal/application/maps"
)

type flakyGeocoder struct {
	err   error
	calls int
}

func (f *flakyGeocoder) Search(context.Context, string, *maps.Coordinates, int, string) ([]maps.Place, error) {
	f.calls++

	return nil, f.err
}

func (f *flakyGeocoder) Reverse(context.Context, maps.Coordinates, string) (maps.Place, error) {
	f.calls++

	return maps.Place{}, f.err
}

func TestAGeocoderThatIsDownIsLeftAloneForAWhile(t *testing.T) {
	inner := &flakyGeocoder{err: fmt.Errorf("%w: call Nominatim: connection refused", maps.ErrUnavailable)}
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	g := NewGuarded(inner, 30*time.Second)
	g.now = func() time.Time { return now }

	if _, err := g.Search(context.Background(), "mall", nil, 5, "ar"); !errors.Is(err, maps.ErrUnavailable) {
		t.Fatalf("first call: %v", err)
	}

	for i := 0; i < 3; i++ {
		if _, err := g.Reverse(context.Background(), maps.Coordinates{}, "ar"); !errors.Is(err, maps.ErrUnavailable) {
			t.Fatalf("resting call: %v", err)
		}
	}

	if inner.calls != 1 {
		t.Fatalf("asked %d times while resting", inner.calls)
	}

	now = now.Add(31 * time.Second)
	inner.err = nil

	if _, err := g.Search(context.Background(), "mall", nil, 5, "ar"); err != nil || inner.calls != 2 {
		t.Fatalf("after the rest: %v, %d calls", err, inner.calls)
	}

	if _, err := g.Search(context.Background(), "mall", nil, 5, "ar"); err != nil || inner.calls != 3 {
		t.Fatalf("up again: %v, %d calls", err, inner.calls)
	}
}

func TestNothingFoundIsNotAFailure(t *testing.T) {
	inner := &flakyGeocoder{err: maps.ErrPlaceNotFound}
	g := NewGuarded(inner, time.Minute)

	for i := 0; i < 3; i++ {
		if _, err := g.Reverse(context.Background(), maps.Coordinates{}, "ar"); !errors.Is(err, maps.ErrPlaceNotFound) {
			t.Fatal(err)
		}
	}

	if inner.calls != 3 {
		t.Fatalf("%d calls", inner.calls)
	}
}

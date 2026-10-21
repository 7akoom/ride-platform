package geocoding

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/7akoom/ride-platform/services/location-service/internal/application/maps"
)

// DefaultRestAfterFailure is how long a geocoder that just failed is left alone.
const DefaultRestAfterFailure = 30 * time.Second

// Guarded stops asking a geocoder that is down for a while after it fails, so a
// search does not wait for its timeout every time: the other sources answer at
// once instead. One call goes through when the rest is over; if it works, every
// call goes through again.
type Guarded struct {
	inner maps.Geocoder
	rest  time.Duration
	now   func() time.Time

	mu         sync.Mutex
	restingTil time.Time
}

var _ maps.Geocoder = (*Guarded)(nil)

func NewGuarded(inner maps.Geocoder, rest time.Duration) *Guarded {
	if inner == nil {
		panic("geocoder is required")
	}

	if rest <= 0 {
		rest = DefaultRestAfterFailure
	}

	return &Guarded{inner: inner, rest: rest, now: time.Now}
}

func (g *Guarded) Search(ctx context.Context, query string, near *maps.Coordinates, limit int, languages string) ([]maps.Place, error) {
	if !g.allowed() {
		return nil, errResting
	}

	places, err := g.inner.Search(ctx, query, near, limit, languages)
	g.record(err)

	return places, err
}

func (g *Guarded) Reverse(ctx context.Context, at maps.Coordinates, languages string) (maps.Place, error) {
	if !g.allowed() {
		return maps.Place{}, errResting
	}

	place, err := g.inner.Reverse(ctx, at, languages)
	g.record(err)

	return place, err
}

var errResting = restingError{}

type restingError struct{}

func (restingError) Error() string { return "the map service failed a moment ago; not asked again yet" }
func (restingError) Unwrap() error { return maps.ErrUnavailable }

func (g *Guarded) allowed() bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := g.now()
	if now.Before(g.restingTil) {
		return false
	}

	// One call tries again; the others wait for its answer's rest to be set.
	if !g.restingTil.IsZero() {
		g.restingTil = now.Add(g.rest)
	}

	return true
}

func (g *Guarded) record(err error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Any answer from the geocoder, "nothing found" too, means it is up.
	if errors.Is(err, maps.ErrUnavailable) {
		g.restingTil = g.now().Add(g.rest)
	} else {
		g.restingTil = time.Time{}
	}
}

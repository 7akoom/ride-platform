package grpc

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

type CallerResolver interface {
	RiderID(ctx context.Context, identityID string) (string, error)
	DriverID(ctx context.Context, identityID string) (string, error)
}

type TripReader interface {
	GetTrip(ctx context.Context, tripID string) (trip.Trip, error)
}

type caller struct {
	identityID string
	resolver   CallerResolver
	trips      TripReader
}

func (c caller) ownsRider(ctx context.Context, riderID string) (bool, error) {
	if riderID == "" {
		return false, nil
	}

	id, err := c.resolver.RiderID(ctx, c.identityID)
	if err != nil {
		return false, err
	}

	return id != "" && id == riderID, nil
}

func (c caller) ownsDriver(ctx context.Context, driverID string) (bool, error) {
	if driverID == "" {
		return false, nil
	}

	id, err := c.resolver.DriverID(ctx, c.identityID)
	if err != nil {
		return false, err
	}

	return id != "" && id == driverID, nil
}

// participation reports whether the caller is the rider or the driver of the
// trip. A trip that does not exist counts as no participation, so callers
// cannot tell a missing trip from someone else's.
type participation struct {
	rider  bool
	driver bool
}

func (c caller) participation(ctx context.Context, tripID string) (participation, error) {
	if tripID == "" || c.trips == nil {
		return participation{}, nil
	}

	found, err := c.trips.GetTrip(ctx, tripID)
	if errors.Is(err, trip.ErrTripNotFound) {
		return participation{}, nil
	}

	if err != nil {
		return participation{}, err
	}

	var result participation

	if found.RiderID != "" {
		if result.rider, err = c.ownsRider(ctx, found.RiderID); err != nil {
			return participation{}, err
		}
	}

	if found.DriverID != "" {
		if result.driver, err = c.ownsDriver(ctx, found.DriverID); err != nil {
			return participation{}, err
		}
	}

	return result, nil
}

const (
	resolverHitTTL     = time.Minute
	resolverMissTTL    = 5 * time.Second
	resolverMaxEntries = 10000
)

type cacheEntry struct {
	id      string
	expires time.Time
}

type cachingResolver struct {
	next CallerResolver
	now  func() time.Time

	mu      sync.Mutex
	riders  map[string]cacheEntry
	drivers map[string]cacheEntry
}

func NewCachingResolver(next CallerResolver) CallerResolver {
	if next == nil {
		panic("caller resolver is required")
	}

	return &cachingResolver{
		next:    next,
		now:     time.Now,
		riders:  map[string]cacheEntry{},
		drivers: map[string]cacheEntry{},
	}
}

func (r *cachingResolver) RiderID(ctx context.Context, identityID string) (string, error) {
	return r.lookup(ctx, r.riders, identityID, r.next.RiderID)
}

func (r *cachingResolver) DriverID(ctx context.Context, identityID string) (string, error) {
	return r.lookup(ctx, r.drivers, identityID, r.next.DriverID)
}

func (r *cachingResolver) lookup(
	ctx context.Context,
	cache map[string]cacheEntry,
	identityID string,
	fetch func(context.Context, string) (string, error),
) (string, error) {
	r.mu.Lock()
	entry, found := cache[identityID]
	r.mu.Unlock()

	if found && r.now().Before(entry.expires) {
		return entry.id, nil
	}

	id, err := fetch(ctx, identityID)
	if err != nil {
		return "", err
	}

	ttl := resolverHitTTL
	if id == "" {
		ttl = resolverMissTTL
	}

	r.mu.Lock()
	if len(cache) >= resolverMaxEntries {
		clear(cache)
	}

	cache[identityID] = cacheEntry{id: id, expires: r.now().Add(ttl)}
	r.mu.Unlock()

	return id, nil
}

package grpc

import (
	"context"
	"sync"
	"time"
)

type CallerResolver interface {
	RiderID(ctx context.Context, identityID string) (string, error)
	DriverID(ctx context.Context, identityID string) (string, error)
}

type caller struct {
	identityID string
	resolver   CallerResolver
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

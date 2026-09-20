package grpc

import (
	"context"
	"sync"
	"time"
)

// CallerResolver turns an authenticated identity into the rider or driver
// profile id that identity owns. An empty id with no error means the identity
// has no such profile.
type CallerResolver interface {
	RiderID(ctx context.Context, identityID string) (string, error)
	DriverID(ctx context.Context, identityID string) (string, error)
}

// caller is the authenticated end user making the request.
type caller struct {
	identityID string
	resolver   CallerResolver
	devices    DeviceOwnerReader
}

// ownsRider reports whether riderID is the caller's own rider profile.
func (c caller) ownsRider(ctx context.Context, riderID string) (bool, error) {
	if riderID == "" || c.identityID == "" || c.resolver == nil {
		return false, nil
	}

	id, err := c.resolver.RiderID(ctx, c.identityID)
	if err != nil {
		return false, err
	}

	return id != "" && id == riderID, nil
}

// ownsDriver reports whether driverID is the caller's own driver profile.
func (c caller) ownsDriver(ctx context.Context, driverID string) (bool, error) {
	if driverID == "" || c.identityID == "" || c.resolver == nil {
		return false, nil
	}

	id, err := c.resolver.DriverID(ctx, c.identityID)
	if err != nil {
		return false, err
	}

	return id != "" && id == driverID, nil
}

// Drivers report their position every few seconds, so the identity-to-profile
// lookup is cached: one call per identity per TTL, not one per ping. A found
// profile is remembered for a minute; "no profile" only for a few seconds so a
// newly created profile works quickly. Errors are never cached.
const (
	resolverHitTTL     = time.Minute
	resolverMissTTL    = 5 * time.Second
	resolverMaxEntries = 10000
)

type resolverCacheEntry struct {
	id      string
	expires time.Time
}

type cachingResolver struct {
	next CallerResolver
	now  func() time.Time

	mu      sync.Mutex
	riders  map[string]resolverCacheEntry
	drivers map[string]resolverCacheEntry
}

func NewCachingResolver(next CallerResolver) CallerResolver {
	if next == nil {
		panic("caller resolver is required")
	}

	return &cachingResolver{
		next:    next,
		now:     time.Now,
		riders:  map[string]resolverCacheEntry{},
		drivers: map[string]resolverCacheEntry{},
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
	cache map[string]resolverCacheEntry,
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

	cache[identityID] = resolverCacheEntry{id: id, expires: r.now().Add(ttl)}
	r.mu.Unlock()

	return id, nil
}

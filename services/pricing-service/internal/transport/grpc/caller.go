package grpc

import (
	"context"
	"sync"
	"time"
)

// CallerResolver turns an authenticated identity into the rider profile id
// that identity owns. An empty id with no error means the identity has no
// rider profile.
type CallerResolver interface {
	RiderID(ctx context.Context, identityID string) (string, error)
}

// caller is the authenticated end user making the request.
type caller struct {
	identityID string
	resolver   CallerResolver
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

// The identity-to-profile lookup is cached: one call per identity per TTL, not
// one per request. A found profile is remembered for a minute; "no profile"
// only for a few seconds so a newly created profile works quickly. Errors are
// never cached.
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

	mu     sync.Mutex
	riders map[string]resolverCacheEntry
}

func NewCachingResolver(next CallerResolver) CallerResolver {
	if next == nil {
		panic("caller resolver is required")
	}

	return &cachingResolver{
		next:   next,
		now:    time.Now,
		riders: map[string]resolverCacheEntry{},
	}
}

func (r *cachingResolver) RiderID(ctx context.Context, identityID string) (string, error) {
	r.mu.Lock()
	entry, found := r.riders[identityID]
	r.mu.Unlock()

	if found && r.now().Before(entry.expires) {
		return entry.id, nil
	}

	id, err := r.next.RiderID(ctx, identityID)
	if err != nil {
		return "", err
	}

	ttl := resolverHitTTL
	if id == "" {
		ttl = resolverMissTTL
	}

	r.mu.Lock()
	if len(r.riders) >= resolverMaxEntries {
		clear(r.riders)
	}

	r.riders[identityID] = resolverCacheEntry{id: id, expires: r.now().Add(ttl)}
	r.mu.Unlock()

	return id, nil
}

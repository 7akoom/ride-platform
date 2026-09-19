package grpc

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type resolverTestCounter struct {
	riders  atomic.Int32
	drivers atomic.Int32
	riderID string
	err     error
}

func (c *resolverTestCounter) RiderID(context.Context, string) (string, error) {
	c.riders.Add(1)

	return c.riderID, c.err
}

func (c *resolverTestCounter) DriverID(context.Context, string) (string, error) {
	c.drivers.Add(1)

	return "driver-x", c.err
}

func newResolverTestCache(next CallerResolver) (*cachingResolver, *time.Time) {
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	resolver := NewCachingResolver(next).(*cachingResolver)
	resolver.now = func() time.Time { return now }

	return resolver, &now
}

func TestCachingResolverServesRepeatedLookupsFromMemory(t *testing.T) {
	next := &resolverTestCounter{riderID: "rider-a"}
	resolver, _ := newResolverTestCache(next)

	for range 5 {
		if id, err := resolver.RiderID(context.Background(), "identity-a"); err != nil || id != "rider-a" {
			t.Fatalf("got %q, %v", id, err)
		}
	}

	if next.riders.Load() != 1 {
		t.Fatalf("expected one upstream lookup, got %d", next.riders.Load())
	}
}

func TestCachingResolverExpiresHitsAfterAMinute(t *testing.T) {
	next := &resolverTestCounter{riderID: "rider-a"}
	resolver, now := newResolverTestCache(next)

	_, _ = resolver.RiderID(context.Background(), "identity-a")
	*now = now.Add(resolverHitTTL + time.Second)
	_, _ = resolver.RiderID(context.Background(), "identity-a")

	if next.riders.Load() != 2 {
		t.Fatalf("expected a fresh lookup after the TTL, got %d", next.riders.Load())
	}
}

func TestCachingResolverRemembersMissesBrieflyOnly(t *testing.T) {
	next := &resolverTestCounter{}
	resolver, now := newResolverTestCache(next)

	_, _ = resolver.RiderID(context.Background(), "identity-new")
	_, _ = resolver.RiderID(context.Background(), "identity-new")

	if next.riders.Load() != 1 {
		t.Fatalf("a miss should be cached briefly, got %d lookups", next.riders.Load())
	}

	*now = now.Add(resolverMissTTL + time.Second)
	next.riderID = "rider-new"

	if id, _ := resolver.RiderID(context.Background(), "identity-new"); id != "rider-new" {
		t.Fatalf("a newly created profile must be found soon after, got %q", id)
	}
}

func TestCachingResolverNeverCachesErrors(t *testing.T) {
	next := &resolverTestCounter{err: errors.New("down")}
	resolver, _ := newResolverTestCache(next)

	for range 3 {
		if _, err := resolver.RiderID(context.Background(), "identity-a"); err == nil {
			t.Fatalf("expected the upstream error")
		}
	}

	if next.riders.Load() != 3 {
		t.Fatalf("errors must not be cached, got %d lookups", next.riders.Load())
	}
}

func TestCachingResolverKeepsRidersAndDriversApart(t *testing.T) {
	next := &resolverTestCounter{riderID: "rider-a"}
	resolver, _ := newResolverTestCache(next)

	rider, _ := resolver.RiderID(context.Background(), "identity-a")
	driver, _ := resolver.DriverID(context.Background(), "identity-a")

	if rider != "rider-a" || driver != "driver-x" {
		t.Fatalf("got rider=%q driver=%q", rider, driver)
	}
}

func TestCachingResolverIsSafeForConcurrentUse(t *testing.T) {
	next := &resolverTestCounter{riderID: "rider-a"}
	resolver := NewCachingResolver(next)

	var wg sync.WaitGroup

	for i := range 50 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			identity := "identity-a"
			if i%2 == 0 {
				identity = "identity-b"
			}

			_, _ = resolver.RiderID(context.Background(), identity)
			_, _ = resolver.DriverID(context.Background(), identity)
		}()
	}

	wg.Wait()
}

func TestCachingResolverStaysBounded(t *testing.T) {
	next := &resolverTestCounter{riderID: "rider-a"}
	resolver, _ := newResolverTestCache(next)

	for i := range resolverMaxEntries + 50 {
		_, _ = resolver.RiderID(context.Background(), string(rune('a'+i%26))+time.Duration(i).String())
	}

	if len(resolver.riders) > resolverMaxEntries {
		t.Fatalf("cache grew to %d entries", len(resolver.riders))
	}
}

package grpc

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"
)

type resolverCacheCounter struct {
	riderCalls  int
	driverCalls int
	riderID     string
	driverID    string
	err         error
}

func (c *resolverCacheCounter) RiderID(context.Context, string) (string, error) {
	c.riderCalls++

	return c.riderID, c.err
}

func (c *resolverCacheCounter) DriverID(context.Context, string) (string, error) {
	c.driverCalls++

	return c.driverID, c.err
}

func newResolverCacheUnderTest(next CallerResolver) (*cachingResolver, *time.Time) {
	clock := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

	resolver := NewCachingResolver(next).(*cachingResolver)
	resolver.now = func() time.Time { return clock }

	return resolver, &clock
}

func TestResolverCacheAnswersRepeatedLookupsFromMemory(t *testing.T) {
	next := &resolverCacheCounter{driverID: "driver-a"}
	resolver, _ := newResolverCacheUnderTest(next)

	for i := 0; i < 5; i++ {
		id, err := resolver.DriverID(context.Background(), "identity-a")
		if err != nil || id != "driver-a" {
			t.Fatalf("lookup %d: got %q, %v", i, id, err)
		}
	}

	if next.driverCalls != 1 {
		t.Errorf("expected one upstream call for five lookups, got %d", next.driverCalls)
	}
}

func TestResolverCacheKeepsRidersAndDriversApart(t *testing.T) {
	next := &resolverCacheCounter{riderID: "rider-a", driverID: "driver-a"}
	resolver, _ := newResolverCacheUnderTest(next)

	rider, _ := resolver.RiderID(context.Background(), "identity-a")
	driver, _ := resolver.DriverID(context.Background(), "identity-a")

	if rider != "rider-a" || driver != "driver-a" {
		t.Errorf("rider and driver answers were mixed up: %q, %q", rider, driver)
	}
}

func TestResolverCacheHitExpires(t *testing.T) {
	next := &resolverCacheCounter{driverID: "driver-a"}
	resolver, clock := newResolverCacheUnderTest(next)

	_, _ = resolver.DriverID(context.Background(), "identity-a")

	*clock = clock.Add(resolverHitTTL - time.Second)
	_, _ = resolver.DriverID(context.Background(), "identity-a")

	if next.driverCalls != 1 {
		t.Fatalf("a hit inside its TTL must not be refetched, got %d calls", next.driverCalls)
	}

	*clock = clock.Add(2 * time.Second)
	_, _ = resolver.DriverID(context.Background(), "identity-a")

	if next.driverCalls != 2 {
		t.Errorf("a hit past its TTL must be refetched, got %d calls", next.driverCalls)
	}
}

func TestResolverCacheRemembersNoProfileOnlyBriefly(t *testing.T) {
	next := &resolverCacheCounter{}
	resolver, clock := newResolverCacheUnderTest(next)

	_, _ = resolver.DriverID(context.Background(), "identity-new")
	_, _ = resolver.DriverID(context.Background(), "identity-new")

	if next.driverCalls != 1 {
		t.Fatalf("a miss inside its TTL must not be refetched, got %d calls", next.driverCalls)
	}

	// The profile gets created a moment later; it must be found soon.
	next.driverID = "driver-new"
	*clock = clock.Add(resolverMissTTL + time.Second)

	id, err := resolver.DriverID(context.Background(), "identity-new")
	if err != nil || id != "driver-new" {
		t.Errorf("a new profile must be found after the miss TTL, got %q, %v", id, err)
	}
}

func TestResolverCacheNeverRemembersErrors(t *testing.T) {
	next := &resolverCacheCounter{err: errors.New("driver-service down")}
	resolver, _ := newResolverCacheUnderTest(next)

	for i := 0; i < 3; i++ {
		if _, err := resolver.DriverID(context.Background(), "identity-a"); err == nil {
			t.Fatal("expected the upstream error")
		}
	}

	if next.driverCalls != 3 {
		t.Errorf("errors must not be cached, got %d upstream calls for 3 lookups", next.driverCalls)
	}

	next.err = nil
	next.driverID = "driver-a"

	if id, err := resolver.DriverID(context.Background(), "identity-a"); err != nil || id != "driver-a" {
		t.Errorf("the first success after an outage must be returned, got %q, %v", id, err)
	}
}

func TestResolverCacheStaysBounded(t *testing.T) {
	next := &resolverCacheCounter{driverID: "driver-a"}
	resolver, _ := newResolverCacheUnderTest(next)

	for i := 0; i <= resolverMaxEntries; i++ {
		if _, err := resolver.DriverID(context.Background(), "identity-"+strconv.Itoa(i)); err != nil {
			t.Fatal(err)
		}
	}

	resolver.mu.Lock()
	size := len(resolver.drivers)
	resolver.mu.Unlock()

	if size > resolverMaxEntries {
		t.Errorf("the cache grew past its bound: %d entries", size)
	}
}

func TestResolverCacheRequiresAnUpstream(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected a panic without an upstream resolver")
		}
	}()

	NewCachingResolver(nil)
}

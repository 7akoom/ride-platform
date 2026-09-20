package trip

import (
	"context"
	"testing"
)

type capabilityProbe interface{ Probe() string }

type capabilityBase struct{ Service }

type capabilityLayer struct {
	Service

	name string
}

func (l *capabilityLayer) Probe() string   { return l.name }
func (l *capabilityLayer) Unwrap() Service { return l.Service }

// A layer that wraps another Service but does not say so.
type capabilityOpaque struct{ Service }

func TestAsFindsTheCapabilityOnTheOutermostService(t *testing.T) {
	stack := &capabilityLayer{Service: &capabilityBase{}, name: "outer"}

	probe, ok := As[capabilityProbe](stack)
	if !ok || probe.Probe() != "outer" {
		t.Errorf("expected the outer layer, got %v (found=%v)", probe, ok)
	}
}

func TestAsLooksThroughDecoratorsThatWrapTheOneWithTheCapability(t *testing.T) {
	inner := &capabilityLayer{Service: &capabilityBase{}, name: "inner"}
	outer := &capabilityLayer2{Service: inner}

	probe, ok := As[capabilityProbe](outer)
	if !ok || probe.Probe() != "inner" {
		t.Errorf("expected to reach the inner layer through the outer one, got %v (found=%v)", probe, ok)
	}
}

// capabilityLayer2 wraps a Service, has no capability of its own, and says what it wraps.
type capabilityLayer2 struct{ Service }

func (l *capabilityLayer2) Unwrap() Service { return l.Service }

func TestAsStopsAtADecoratorThatDoesNotSayWhatItWraps(t *testing.T) {
	stack := &capabilityOpaque{Service: &capabilityLayer{Service: &capabilityBase{}, name: "hidden"}}

	if _, ok := As[capabilityProbe](stack); ok {
		t.Error("a capability behind a decorator that cannot be unwrapped must not be found")
	}
}

func TestAsReturnsNothingForAServiceWithoutTheCapability(t *testing.T) {
	if _, ok := As[capabilityProbe](&capabilityBase{}); ok {
		t.Error("no layer has the capability")
	}

	if _, ok := As[capabilityProbe](nil); ok {
		t.Error("a nil Service has no capabilities")
	}
}

type capabilityLoop struct{ Service }

func (l *capabilityLoop) Unwrap() Service { return l }

func TestAsGivesUpOnADecoratorThatWrapsItself(t *testing.T) {
	if _, ok := As[capabilityProbe](&capabilityLoop{}); ok {
		t.Error("a loop has no capability")
	}
}

func TestTheTrackingDecoratorCanBeUnwrapped(t *testing.T) {
	base := &trackingFakeBase{}
	wrapped := WithDriverTracking(base, &trackingFakeLocator{})

	unwrapper, ok := wrapped.(Unwrapper)
	if !ok || unwrapper.Unwrap() != Service(base) {
		t.Fatalf("the tracking decorator must expose the Service it wraps")
	}

	// And a decorator stacked on top of it still reaches GetDriverLocation.
	stacked := &capabilityLayer2{Service: wrapped}

	type tracker interface {
		GetDriverLocation(ctx context.Context, tripID string) (DriverLocation, error)
	}

	if _, ok := As[tracker](stacked); !ok {
		t.Error("driver tracking must stay reachable when another decorator wraps it")
	}
}

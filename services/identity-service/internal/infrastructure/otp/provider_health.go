package otp

import (
	"errors"
	"strings"
	"sync"
	"time"
)

type ProviderHealthTracker interface {
	CanAttempt(
		channel DeliveryTrackingChannel,
		provider DeliveryTrackingProvider,
		at time.Time,
	) bool

	RecordSuccess(
		channel DeliveryTrackingChannel,
		provider DeliveryTrackingProvider,
	)

	RecordFailure(
		channel DeliveryTrackingChannel,
		provider DeliveryTrackingProvider,
		at time.Time,
	)
}

type NoopProviderHealthTracker struct{}

func (NoopProviderHealthTracker) CanAttempt(
	DeliveryTrackingChannel,
	DeliveryTrackingProvider,
	time.Time,
) bool {
	return true
}

func (NoopProviderHealthTracker) RecordSuccess(
	DeliveryTrackingChannel,
	DeliveryTrackingProvider,
) {
}

func (NoopProviderHealthTracker) RecordFailure(
	DeliveryTrackingChannel,
	DeliveryTrackingProvider,
	time.Time,
) {
}

type CircuitBreakerProviderHealthTracker struct {
	mu               sync.Mutex
	failureThreshold int
	cooldown         time.Duration
	states           map[providerHealthKey]providerHealthState
}

type providerHealthKey struct {
	channel  DeliveryTrackingChannel
	provider DeliveryTrackingProvider
}

type providerHealthState struct {
	consecutiveFailures int
	openUntil           time.Time
}

func NewCircuitBreakerProviderHealthTracker(
	failureThreshold int,
	cooldown time.Duration,
) (*CircuitBreakerProviderHealthTracker, error) {
	if failureThreshold <= 0 {
		return nil, errors.New(
			"provider health failure threshold must be greater than zero",
		)
	}

	if cooldown <= 0 {
		return nil, errors.New(
			"provider health cooldown must be greater than zero",
		)
	}

	return &CircuitBreakerProviderHealthTracker{
		failureThreshold: failureThreshold,
		cooldown:         cooldown,
		states: make(
			map[providerHealthKey]providerHealthState,
		),
	}, nil
}

func (t *CircuitBreakerProviderHealthTracker) CanAttempt(
	channel DeliveryTrackingChannel,
	provider DeliveryTrackingProvider,
	at time.Time,
) bool {
	if t == nil {
		return true
	}

	key, ok := normalizeProviderHealthKey(
		channel,
		provider,
	)
	if !ok {
		return false
	}

	if at.IsZero() {
		at = time.Now().UTC()
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	state, exists := t.states[key]
	if !exists {
		return true
	}

	if state.openUntil.IsZero() {
		return true
	}

	return !at.Before(
		state.openUntil,
	)
}

func (t *CircuitBreakerProviderHealthTracker) RecordSuccess(
	channel DeliveryTrackingChannel,
	provider DeliveryTrackingProvider,
) {
	if t == nil {
		return
	}

	key, ok := normalizeProviderHealthKey(
		channel,
		provider,
	)
	if !ok {
		return
	}

	t.mu.Lock()
	delete(
		t.states,
		key,
	)
	t.mu.Unlock()
}

func (t *CircuitBreakerProviderHealthTracker) RecordFailure(
	channel DeliveryTrackingChannel,
	provider DeliveryTrackingProvider,
	at time.Time,
) {
	if t == nil {
		return
	}

	key, ok := normalizeProviderHealthKey(
		channel,
		provider,
	)
	if !ok {
		return
	}

	if at.IsZero() {
		at = time.Now().UTC()
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	state := t.states[key]

	state.consecutiveFailures++

	if state.consecutiveFailures >=
		t.failureThreshold {
		state.openUntil = at.Add(
			t.cooldown,
		)
	}

	t.states[key] = state
}

func normalizeProviderHealthKey(
	channel DeliveryTrackingChannel,
	provider DeliveryTrackingProvider,
) (providerHealthKey, bool) {
	normalizedChannel := DeliveryTrackingChannel(
		strings.ToLower(
			strings.TrimSpace(
				string(channel),
			),
		),
	)

	normalizedProvider := DeliveryTrackingProvider(
		strings.ToLower(
			strings.TrimSpace(
				string(provider),
			),
		),
	)

	if normalizedChannel == "" ||
		normalizedProvider == "" {
		return providerHealthKey{}, false
	}

	return providerHealthKey{
		channel:  normalizedChannel,
		provider: normalizedProvider,
	}, true
}

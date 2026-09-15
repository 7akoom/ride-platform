package outbox_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/outbox"
)

// --- test doubles -----------------------------------------------------

type fakeClock struct {
	now time.Time
}

func (c *fakeClock) Now() time.Time { return c.now }

type fakeStore struct {
	claimResults []outbox.Event
	claimErr     error

	markPublishedResult bool
	markPublishedErr    error
	markPublishedCalls  []outbox.MarkPublishedInput

	markFailedResult bool
	markFailedErr    error
	markFailedCalls  []outbox.MarkFailedInput
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		markPublishedResult: true,
		markFailedResult:    true,
	}
}

func (s *fakeStore) ClaimPending(
	_ context.Context,
	_ outbox.ClaimPendingInput,
) ([]outbox.Event, error) {
	if s.claimErr != nil {
		return nil, s.claimErr
	}

	return s.claimResults, nil
}

func (s *fakeStore) MarkPublished(
	_ context.Context,
	input outbox.MarkPublishedInput,
) (bool, error) {
	s.markPublishedCalls = append(s.markPublishedCalls, input)

	if s.markPublishedErr != nil {
		return false, s.markPublishedErr
	}

	return s.markPublishedResult, nil
}

func (s *fakeStore) MarkFailed(
	_ context.Context,
	input outbox.MarkFailedInput,
) (bool, error) {
	s.markFailedCalls = append(s.markFailedCalls, input)

	if s.markFailedErr != nil {
		return false, s.markFailedErr
	}

	return s.markFailedResult, nil
}

type fakePublisher struct {
	failFor  map[string]error
	messages []outbox.Message
}

func (p *fakePublisher) Publish(
	_ context.Context,
	message outbox.Message,
) error {
	p.messages = append(p.messages, message)

	if err, ok := p.failFor[message.ID]; ok {
		return err
	}

	return nil
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testConfig() outbox.ProcessorConfig {
	return outbox.ProcessorConfig{
		BatchSize:         10,
		LeaseDuration:     30 * time.Second,
		InitialRetryDelay: 1 * time.Second,
		MaxRetryDelay:     16 * time.Second,
	}
}

// --- NewProcessor -------------------------------------------------------

func TestNewProcessor_PanicsOnInvalidArguments(t *testing.T) {
	valid := testConfig()

	cases := map[string]struct {
		store     outbox.Store
		publisher outbox.Publisher
		clock     outbox.Clock
		config    outbox.ProcessorConfig
	}{
		"nil store":         {nil, &fakePublisher{}, &fakeClock{}, valid},
		"nil publisher":     {newFakeStore(), nil, &fakeClock{}, valid},
		"nil clock":         {newFakeStore(), &fakePublisher{}, nil, valid},
		"zero batch size":   {newFakeStore(), &fakePublisher{}, &fakeClock{}, withBatchSize(valid, 0)},
		"zero lease":        {newFakeStore(), &fakePublisher{}, &fakeClock{}, withLease(valid, 0)},
		"zero initial delay": {newFakeStore(), &fakePublisher{}, &fakeClock{}, withInitialDelay(valid, 0)},
		"zero max delay":     {newFakeStore(), &fakePublisher{}, &fakeClock{}, withMaxDelay(valid, 0)},
		"initial exceeds max": {
			newFakeStore(), &fakePublisher{}, &fakeClock{},
			outbox.ProcessorConfig{
				BatchSize:         1,
				LeaseDuration:     time.Second,
				InitialRetryDelay: 10 * time.Second,
				MaxRetryDelay:     time.Second,
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatalf("expected NewProcessor to panic for %q", name)
				}
			}()

			outbox.NewProcessor(tc.store, tc.publisher, tc.clock, tc.config)
		})
	}
}

func withBatchSize(c outbox.ProcessorConfig, v int) outbox.ProcessorConfig {
	c.BatchSize = v
	return c
}

func withLease(c outbox.ProcessorConfig, v time.Duration) outbox.ProcessorConfig {
	c.LeaseDuration = v
	return c
}

func withInitialDelay(c outbox.ProcessorConfig, v time.Duration) outbox.ProcessorConfig {
	c.InitialRetryDelay = v
	return c
}

func withMaxDelay(c outbox.ProcessorConfig, v time.Duration) outbox.ProcessorConfig {
	c.MaxRetryDelay = v
	return c
}

// --- ProcessOnce ---------------------------------------------------------

func TestProcessor_ProcessOnce_PublishesAllClaimedEvents(t *testing.T) {
	store := newFakeStore()
	store.claimResults = []outbox.Event{
		{ID: "evt-1", EventType: "thing.happened", Payload: []byte(`{"a":1}`)},
		{ID: "evt-2", EventType: "thing.happened", Payload: []byte(`{"a":2}`)},
	}
	publisher := &fakePublisher{}
	clock := &fakeClock{now: time.Unix(1_700_000_000, 0)}

	processor := outbox.NewProcessor(store, publisher, clock, testConfig())

	result, err := processor.ProcessOnce(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Claimed != 2 || result.Published != 2 || result.RetryScheduled != 0 || result.LostClaims != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}

	if len(publisher.messages) != 2 {
		t.Fatalf("expected 2 messages published, got %d", len(publisher.messages))
	}

	if len(store.markPublishedCalls) != 2 {
		t.Fatalf("expected 2 MarkPublished calls, got %d", len(store.markPublishedCalls))
	}
}

func TestProcessor_ProcessOnce_WrapsClaimError(t *testing.T) {
	store := newFakeStore()
	store.claimErr = errors.New("connection refused")

	processor := outbox.NewProcessor(store, &fakePublisher{}, &fakeClock{}, testConfig())

	_, err := processor.ProcessOnce(context.Background())
	if err == nil {
		t.Fatal("expected an error")
	}

	if !errors.Is(err, store.claimErr) {
		t.Fatalf("expected wrapped claim error, got: %v", err)
	}
}

func TestProcessor_ProcessOnce_SchedulesRetryOnPublishFailure(t *testing.T) {
	store := newFakeStore()
	store.claimResults = []outbox.Event{
		{ID: "evt-1", ClaimToken: "tok-1", PublishAttempts: 1},
	}
	publisher := &fakePublisher{
		failFor: map[string]error{"evt-1": errors.New("broker unreachable")},
	}
	now := time.Unix(1_700_000_000, 0)
	clock := &fakeClock{now: now}

	processor := outbox.NewProcessor(store, publisher, clock, testConfig())

	result, err := processor.ProcessOnce(context.Background())
	if err == nil {
		t.Fatal("expected ProcessOnce to report the publish failure")
	}

	if result.Published != 0 || result.RetryScheduled != 1 || result.LostClaims != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}

	if len(store.markFailedCalls) != 1 {
		t.Fatalf("expected 1 MarkFailed call, got %d", len(store.markFailedCalls))
	}

	call := store.markFailedCalls[0]
	if call.EventID != "evt-1" || call.ClaimToken != "tok-1" {
		t.Fatalf("unexpected MarkFailed input: %+v", call)
	}

	// PublishAttempts=1 -> first failure -> InitialRetryDelay.
	wantRetryAt := now.Add(testConfig().InitialRetryDelay)
	if !call.RetryAt.Equal(wantRetryAt) {
		t.Fatalf("expected retry at %v, got %v", wantRetryAt, call.RetryAt)
	}
}

func TestProcessor_ProcessOnce_RetryDelayBacksOffExponentiallyUpToMax(t *testing.T) {
	config := outbox.ProcessorConfig{
		BatchSize:         10,
		LeaseDuration:     30 * time.Second,
		InitialRetryDelay: 1 * time.Second,
		MaxRetryDelay:     16 * time.Second,
	}

	// publishAttempts as reported on the claimed event (attempts so far,
	// i.e. including this one) -> expected delay before the next retry.
	cases := map[int]time.Duration{
		1: 1 * time.Second,
		2: 2 * time.Second,
		3: 4 * time.Second,
		4: 8 * time.Second,
		5: 16 * time.Second,
		6: 16 * time.Second, // capped
		9: 16 * time.Second, // stays capped
	}

	for attempts, want := range cases {
		store := newFakeStore()
		store.claimResults = []outbox.Event{
			{ID: "evt-1", ClaimToken: "tok-1", PublishAttempts: attempts},
		}
		publisher := &fakePublisher{failFor: map[string]error{"evt-1": errors.New("nope")}}
		now := time.Unix(1_700_000_000, 0)
		clock := &fakeClock{now: now}

		processor := outbox.NewProcessor(store, publisher, clock, config)

		if _, err := processor.ProcessOnce(context.Background()); err == nil {
			t.Fatalf("attempts=%d: expected an error", attempts)
		}

		got := store.markFailedCalls[0].RetryAt.Sub(now)
		if got != want {
			t.Errorf("attempts=%d: retry delay = %v, want %v", attempts, got, want)
		}
	}
}

func TestProcessor_ProcessOnce_CountsLostClaimAfterSuccessfulPublish(t *testing.T) {
	store := newFakeStore()
	store.claimResults = []outbox.Event{{ID: "evt-1"}}
	store.markPublishedResult = false // another worker already claimed/published it

	processor := outbox.NewProcessor(store, &fakePublisher{}, &fakeClock{}, testConfig())

	result, err := processor.ProcessOnce(context.Background())
	if err == nil {
		t.Fatal("expected an error describing the lost claim")
	}

	if result.Published != 0 || result.LostClaims != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestProcessor_ProcessOnce_CountsLostClaimAfterFailedPublish(t *testing.T) {
	store := newFakeStore()
	store.claimResults = []outbox.Event{{ID: "evt-1"}}
	store.markFailedResult = false // lease expired before we could record the failure

	publisher := &fakePublisher{failFor: map[string]error{"evt-1": errors.New("nope")}}

	processor := outbox.NewProcessor(store, publisher, &fakeClock{}, testConfig())

	result, err := processor.ProcessOnce(context.Background())
	if err == nil {
		t.Fatal("expected an error describing the lost claim")
	}

	if result.RetryScheduled != 0 || result.LostClaims != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestProcessor_ProcessOnce_StopsOnContextCancellation(t *testing.T) {
	store := newFakeStore()
	store.claimResults = []outbox.Event{
		{ID: "evt-1"}, {ID: "evt-2"}, {ID: "evt-3"},
	}

	ctx, cancel := context.WithCancel(context.Background())

	publisher := &countingCancelPublisher{cancelAfter: 1, cancel: cancel}

	processor := outbox.NewProcessor(store, publisher, &fakeClock{}, testConfig())

	result, err := processor.ProcessOnce(ctx)
	if err == nil {
		t.Fatal("expected an error from the cancelled context")
	}

	if result.Claimed != 3 {
		t.Fatalf("expected all 3 events claimed, got %d", result.Claimed)
	}

	if publisher.calls.Load() >= 3 {
		t.Fatalf("expected processing to stop before all events were published, got %d calls", publisher.calls.Load())
	}
}

// countingCancelPublisher publishes successfully but cancels the context
// after N calls, simulating the caller's context expiring mid-batch.
type countingCancelPublisher struct {
	calls       atomic.Int64
	cancelAfter int64
	cancel      context.CancelFunc
}

func (p *countingCancelPublisher) Publish(
	_ context.Context,
	_ outbox.Message,
) error {
	n := p.calls.Add(1)
	if n >= p.cancelAfter {
		p.cancel()
	}

	return nil
}

// --- Worker ---------------------------------------------------------------

type fakeProcessorRunner struct {
	calls  atomic.Int32
	result outbox.ProcessResult
	err    error
}

func (r *fakeProcessorRunner) ProcessOnce(
	_ context.Context,
) (outbox.ProcessResult, error) {
	r.calls.Add(1)

	return r.result, r.err
}

func TestNewWorker_PanicsOnInvalidArguments(t *testing.T) {
	cases := map[string]struct {
		processor outbox.ProcessorRunner
		logger    *slog.Logger
		config    outbox.WorkerConfig
	}{
		"nil processor":  {nil, testLogger(), outbox.WorkerConfig{PollInterval: time.Second}},
		"nil logger":     {&fakeProcessorRunner{}, nil, outbox.WorkerConfig{PollInterval: time.Second}},
		"zero interval":  {&fakeProcessorRunner{}, testLogger(), outbox.WorkerConfig{PollInterval: 0}},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatalf("expected NewWorker to panic for %q", name)
				}
			}()

			outbox.NewWorker(tc.processor, tc.logger, tc.config)
		})
	}
}

func TestWorker_Run_StopsImmediatelyOnAlreadyCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	runner := &fakeProcessorRunner{}
	worker := outbox.NewWorker(runner, testLogger(), outbox.WorkerConfig{PollInterval: time.Hour})

	if err := worker.Run(ctx); err != nil {
		t.Fatalf("expected nil error on cancelled context, got: %v", err)
	}

	if runner.calls.Load() != 0 {
		t.Fatalf("expected ProcessOnce not to be called, got %d calls", runner.calls.Load())
	}
}

func TestWorker_Run_PollsUntilCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	runner := &cancelingRunner{cancelAfter: 3, cancel: cancel}
	worker := outbox.NewWorker(runner, testLogger(), outbox.WorkerConfig{PollInterval: time.Millisecond})

	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected nil error, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not stop after ProcessOnce triggered cancellation")
	}

	if runner.calls.Load() < 3 {
		t.Fatalf("expected at least 3 ProcessOnce calls, got %d", runner.calls.Load())
	}
}

type cancelingRunner struct {
	calls       atomic.Int32
	cancelAfter int32
	cancel      context.CancelFunc
}

func (r *cancelingRunner) ProcessOnce(
	_ context.Context,
) (outbox.ProcessResult, error) {
	n := r.calls.Add(1)
	if n >= r.cancelAfter {
		r.cancel()
	}

	return outbox.ProcessResult{}, nil
}

package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type processorTestClock struct {
	times []time.Time
	index int
}

func (c *processorTestClock) Now() time.Time {
	if len(c.times) == 0 {
		return time.Time{}
	}

	if c.index >= len(c.times) {
		return c.times[len(c.times)-1]
	}

	current := c.times[c.index]
	c.index++

	return current
}

type processorTestStore struct {
	claimInput ClaimPendingInput
	claimCalls int
	events     []Event
	claimErr   error

	markPublishedInput MarkPublishedInput
	markPublishedCalls int
	markPublished      bool
	markPublishedErr   error

	markFailedInput MarkFailedInput
	markFailedCalls int
	markFailed      bool
	markFailedErr   error
}

func (s *processorTestStore) ClaimPending(
	ctx context.Context,
	input ClaimPendingInput,
) ([]Event, error) {
	s.claimCalls++
	s.claimInput = input

	if s.claimErr != nil {
		return nil, s.claimErr
	}

	return s.events, nil
}

func (s *processorTestStore) MarkPublished(
	ctx context.Context,
	input MarkPublishedInput,
) (bool, error) {
	s.markPublishedCalls++
	s.markPublishedInput = input

	if s.markPublishedErr != nil {
		return false, s.markPublishedErr
	}

	return s.markPublished, nil
}

func (s *processorTestStore) MarkFailed(
	ctx context.Context,
	input MarkFailedInput,
) (bool, error) {
	s.markFailedCalls++
	s.markFailedInput = input

	if s.markFailedErr != nil {
		return false, s.markFailedErr
	}

	return s.markFailed, nil
}

type processorTestPublisher struct {
	calls    int
	messages []Message
	err      error
}

func (p *processorTestPublisher) Publish(
	ctx context.Context,
	message Message,
) error {
	p.calls++
	p.messages = append(
		p.messages,
		message,
	)

	return p.err
}

func validProcessorTestConfig() ProcessorConfig {
	return ProcessorConfig{
		BatchSize:         100,
		LeaseDuration:     30 * time.Second,
		InitialRetryDelay: time.Second,
		MaxRetryDelay:     time.Minute,
	}
}

func processorTestEvent() Event {
	return Event{
		ID:              "11111111-1111-4111-8111-111111111111",
		AggregateType:   "identity",
		AggregateID:     "22222222-2222-4222-8222-222222222222",
		EventType:       "identity.suspended",
		SchemaVersion:   1,
		Payload:         json.RawMessage(`{"identity_id":"22222222-2222-4222-8222-222222222222"}`),
		OccurredAt:      time.Date(2026, time.August, 28, 15, 0, 0, 0, time.UTC),
		ClaimToken:      "33333333-3333-4333-8333-333333333333",
		PublishAttempts: 1,
	}
}

func TestProcessorProcessOncePublishesClaimedEvent(
	t *testing.T,
) {
	claimedAt := time.Date(
		2026,
		time.August,
		28,
		15,
		10,
		0,
		0,
		time.UTC,
	)

	publishedAt := claimedAt.Add(
		2 * time.Second,
	)

	event := processorTestEvent()

	store := &processorTestStore{
		events:        []Event{event},
		markPublished: true,
	}

	publisher := &processorTestPublisher{}

	clock := &processorTestClock{
		times: []time.Time{
			claimedAt,
			publishedAt,
		},
	}

	processor := NewProcessor(
		store,
		publisher,
		clock,
		validProcessorTestConfig(),
	)

	result, err := processor.ProcessOnce(
		context.Background(),
	)
	if err != nil {
		t.Fatalf(
			"ProcessOnce() returned an error: %v",
			err,
		)
	}

	if result != (ProcessResult{
		Claimed:   1,
		Published: 1,
	}) {
		t.Fatalf(
			"ProcessOnce() result = %+v",
			result,
		)
	}

	if store.claimCalls != 1 {
		t.Fatalf(
			"ClaimPending() calls = %d, expected 1",
			store.claimCalls,
		)
	}

	if !store.claimInput.ClaimedAt.Equal(
		claimedAt,
	) {
		t.Fatalf(
			"claim time = %v, expected %v",
			store.claimInput.ClaimedAt,
			claimedAt,
		)
	}

	if store.claimInput.LeaseDuration !=
		30*time.Second {
		t.Fatalf(
			"lease duration = %v, expected %v",
			store.claimInput.LeaseDuration,
			30*time.Second,
		)
	}

	if store.claimInput.Limit != 100 {
		t.Fatalf(
			"claim limit = %d, expected 100",
			store.claimInput.Limit,
		)
	}

	if publisher.calls != 1 {
		t.Fatalf(
			"Publish() calls = %d, expected 1",
			publisher.calls,
		)
	}

	if len(publisher.messages) != 1 {
		t.Fatalf(
			"published messages = %d, expected 1",
			len(publisher.messages),
		)
	}

	message := publisher.messages[0]

	if message.ID != event.ID {
		t.Fatalf(
			"message ID = %q, expected %q",
			message.ID,
			event.ID,
		)
	}

	if message.AggregateType !=
		event.AggregateType {
		t.Fatalf(
			"aggregate type = %q, expected %q",
			message.AggregateType,
			event.AggregateType,
		)
	}

	if message.AggregateID !=
		event.AggregateID {
		t.Fatalf(
			"aggregate ID = %q, expected %q",
			message.AggregateID,
			event.AggregateID,
		)
	}

	if message.EventType !=
		event.EventType {
		t.Fatalf(
			"event type = %q, expected %q",
			message.EventType,
			event.EventType,
		)
	}

	if message.SchemaVersion !=
		event.SchemaVersion {
		t.Fatalf(
			"schema version = %d, expected %d",
			message.SchemaVersion,
			event.SchemaVersion,
		)
	}

	if string(message.Payload) !=
		string(event.Payload) {
		t.Fatalf(
			"payload = %s, expected %s",
			message.Payload,
			event.Payload,
		)
	}

	if !message.OccurredAt.Equal(
		event.OccurredAt,
	) {
		t.Fatalf(
			"occurred at = %v, expected %v",
			message.OccurredAt,
			event.OccurredAt,
		)
	}

	if store.markPublishedCalls != 1 {
		t.Fatalf(
			"MarkPublished() calls = %d, expected 1",
			store.markPublishedCalls,
		)
	}

	if store.markPublishedInput.EventID !=
		event.ID {
		t.Fatalf(
			"published event ID = %q, expected %q",
			store.markPublishedInput.EventID,
			event.ID,
		)
	}

	if store.markPublishedInput.ClaimToken !=
		event.ClaimToken {
		t.Fatalf(
			"published claim token = %q, expected %q",
			store.markPublishedInput.ClaimToken,
			event.ClaimToken,
		)
	}

	if !store.markPublishedInput.PublishedAt.Equal(
		publishedAt,
	) {
		t.Fatalf(
			"published at = %v, expected %v",
			store.markPublishedInput.PublishedAt,
			publishedAt,
		)
	}

	if store.markFailedCalls != 0 {
		t.Fatalf(
			"MarkFailed() calls = %d, expected 0",
			store.markFailedCalls,
		)
	}
}

func TestProcessorProcessOnceSchedulesRetryAfterPublishFailure(
	t *testing.T,
) {
	claimedAt := time.Date(
		2026,
		time.August,
		28,
		16,
		0,
		0,
		0,
		time.UTC,
	)

	failedAt := claimedAt.Add(
		time.Second,
	)

	event := processorTestEvent()
	event.PublishAttempts = 3

	publishErr := errors.New(
		"broker unavailable",
	)

	store := &processorTestStore{
		events:     []Event{event},
		markFailed: true,
	}

	publisher := &processorTestPublisher{
		err: publishErr,
	}

	clock := &processorTestClock{
		times: []time.Time{
			claimedAt,
			failedAt,
		},
	}

	processor := NewProcessor(
		store,
		publisher,
		clock,
		validProcessorTestConfig(),
	)

	result, err := processor.ProcessOnce(
		context.Background(),
	)

	if err == nil {
		t.Fatal(
			"ProcessOnce() did not return publish error",
		)
	}

	if !errors.Is(err, publishErr) {
		t.Fatalf(
			"ProcessOnce() error = %v, expected wrapped %v",
			err,
			publishErr,
		)
	}

	if result != (ProcessResult{
		Claimed:        1,
		RetryScheduled: 1,
	}) {
		t.Fatalf(
			"ProcessOnce() result = %+v",
			result,
		)
	}

	if store.markFailedCalls != 1 {
		t.Fatalf(
			"MarkFailed() calls = %d, expected 1",
			store.markFailedCalls,
		)
	}

	if store.markFailedInput.EventID !=
		event.ID {
		t.Fatalf(
			"failed event ID = %q, expected %q",
			store.markFailedInput.EventID,
			event.ID,
		)
	}

	if store.markFailedInput.ClaimToken !=
		event.ClaimToken {
		t.Fatalf(
			"failed claim token = %q, expected %q",
			store.markFailedInput.ClaimToken,
			event.ClaimToken,
		)
	}

	if !store.markFailedInput.FailedAt.Equal(
		failedAt,
	) {
		t.Fatalf(
			"failed at = %v, expected %v",
			store.markFailedInput.FailedAt,
			failedAt,
		)
	}

	expectedRetryAt := failedAt.Add(
		4 * time.Second,
	)

	if !store.markFailedInput.RetryAt.Equal(
		expectedRetryAt,
	) {
		t.Fatalf(
			"retry at = %v, expected %v",
			store.markFailedInput.RetryAt,
			expectedRetryAt,
		)
	}

	if store.markFailedInput.ErrorMessage !=
		publishErr.Error() {
		t.Fatalf(
			"error message = %q, expected %q",
			store.markFailedInput.ErrorMessage,
			publishErr.Error(),
		)
	}

	if store.markPublishedCalls != 0 {
		t.Fatalf(
			"MarkPublished() calls = %d, expected 0",
			store.markPublishedCalls,
		)
	}
}

func TestProcessorRetryDelayUsesExponentialBackoffAndCap(
	t *testing.T,
) {
	processor := NewProcessor(
		&processorTestStore{},
		&processorTestPublisher{},
		&processorTestClock{
			times: []time.Time{
				time.Now(),
			},
		},
		ProcessorConfig{
			BatchSize:         10,
			LeaseDuration:     time.Minute,
			InitialRetryDelay: time.Second,
			MaxRetryDelay:     8 * time.Second,
		},
	)

	tests := []struct {
		attempts int
		expected time.Duration
	}{
		{
			attempts: 1,
			expected: time.Second,
		},
		{
			attempts: 2,
			expected: 2 * time.Second,
		},
		{
			attempts: 3,
			expected: 4 * time.Second,
		},
		{
			attempts: 4,
			expected: 8 * time.Second,
		},
		{
			attempts: 5,
			expected: 8 * time.Second,
		},
		{
			attempts: 100,
			expected: 8 * time.Second,
		},
	}

	for _, testCase := range tests {
		t.Run(
			"",
			func(t *testing.T) {
				got := processor.retryDelay(
					testCase.attempts,
				)

				if got != testCase.expected {
					t.Fatalf(
						"retryDelay(%d) = %v, expected %v",
						testCase.attempts,
						got,
						testCase.expected,
					)
				}
			},
		)
	}
}

func TestProcessorProcessOnceReportsLostClaimAfterSuccessfulPublish(
	t *testing.T,
) {
	claimedAt := time.Date(
		2026,
		time.August,
		28,
		17,
		0,
		0,
		0,
		time.UTC,
	)

	publishedAt :=
		claimedAt.Add(time.Second)

	store := &processorTestStore{
		events: []Event{
			processorTestEvent(),
		},
		markPublished: false,
	}

	publisher := &processorTestPublisher{}

	clock := &processorTestClock{
		times: []time.Time{
			claimedAt,
			publishedAt,
		},
	}

	processor := NewProcessor(
		store,
		publisher,
		clock,
		validProcessorTestConfig(),
	)

	result, err := processor.ProcessOnce(
		context.Background(),
	)

	if err == nil {
		t.Fatal(
			"ProcessOnce() did not report lost claim",
		)
	}

	if result.Claimed != 1 ||
		result.Published != 0 ||
		result.LostClaims != 1 {
		t.Fatalf(
			"ProcessOnce() result = %+v",
			result,
		)
	}

	if publisher.calls != 1 {
		t.Fatalf(
			"Publish() calls = %d, expected 1",
			publisher.calls,
		)
	}

	if store.markFailedCalls != 0 {
		t.Fatalf(
			"MarkFailed() calls = %d, expected 0",
			store.markFailedCalls,
		)
	}
}

func TestProcessorProcessOnceReportsLostClaimAfterPublishFailure(
	t *testing.T,
) {
	claimedAt := time.Date(
		2026,
		time.August,
		28,
		18,
		0,
		0,
		0,
		time.UTC,
	)

	failedAt :=
		claimedAt.Add(time.Second)

	store := &processorTestStore{
		events: []Event{
			processorTestEvent(),
		},
		markFailed: false,
	}

	publisher := &processorTestPublisher{
		err: errors.New(
			"publish failed",
		),
	}

	clock := &processorTestClock{
		times: []time.Time{
			claimedAt,
			failedAt,
		},
	}

	processor := NewProcessor(
		store,
		publisher,
		clock,
		validProcessorTestConfig(),
	)

	result, err := processor.ProcessOnce(
		context.Background(),
	)

	if err == nil {
		t.Fatal(
			"ProcessOnce() did not report lost claim",
		)
	}

	if result.Claimed != 1 ||
		result.RetryScheduled != 0 ||
		result.LostClaims != 1 {
		t.Fatalf(
			"ProcessOnce() result = %+v",
			result,
		)
	}
}

func TestProcessorProcessOnceReturnsClaimStoreError(
	t *testing.T,
) {
	storeErr := errors.New(
		"database unavailable",
	)

	store := &processorTestStore{
		claimErr: storeErr,
	}

	processor := NewProcessor(
		store,
		&processorTestPublisher{},
		&processorTestClock{
			times: []time.Time{
				time.Now(),
			},
		},
		validProcessorTestConfig(),
	)

	result, err := processor.ProcessOnce(
		context.Background(),
	)

	if !errors.Is(err, storeErr) {
		t.Fatalf(
			"ProcessOnce() error = %v, expected wrapped %v",
			err,
			storeErr,
		)
	}

	if result != (ProcessResult{}) {
		t.Fatalf(
			"ProcessOnce() result = %+v, expected zero value",
			result,
		)
	}
}

func TestProcessorProcessOnceReturnsMarkPublishedStoreError(
	t *testing.T,
) {
	storeErr := errors.New(
		"mark published failed",
	)

	claimedAt := time.Now().UTC()

	store := &processorTestStore{
		events: []Event{
			processorTestEvent(),
		},
		markPublishedErr: storeErr,
	}

	processor := NewProcessor(
		store,
		&processorTestPublisher{},
		&processorTestClock{
			times: []time.Time{
				claimedAt,
				claimedAt.Add(time.Second),
			},
		},
		validProcessorTestConfig(),
	)

	result, err := processor.ProcessOnce(
		context.Background(),
	)

	if !errors.Is(err, storeErr) {
		t.Fatalf(
			"ProcessOnce() error = %v, expected wrapped %v",
			err,
			storeErr,
		)
	}

	if result.Claimed != 1 {
		t.Fatalf(
			"claimed = %d, expected 1",
			result.Claimed,
		)
	}

	if result.Published != 0 {
		t.Fatalf(
			"published = %d, expected 0",
			result.Published,
		)
	}
}

func TestProcessorProcessOnceReturnsMarkFailedStoreError(
	t *testing.T,
) {
	storeErr := errors.New(
		"mark failed unavailable",
	)

	claimedAt := time.Now().UTC()

	store := &processorTestStore{
		events: []Event{
			processorTestEvent(),
		},
		markFailedErr: storeErr,
	}

	processor := NewProcessor(
		store,
		&processorTestPublisher{
			err: errors.New(
				"broker unavailable",
			),
		},
		&processorTestClock{
			times: []time.Time{
				claimedAt,
				claimedAt.Add(time.Second),
			},
		},
		validProcessorTestConfig(),
	)

	result, err := processor.ProcessOnce(
		context.Background(),
	)

	if !errors.Is(err, storeErr) {
		t.Fatalf(
			"ProcessOnce() error = %v, expected wrapped %v",
			err,
			storeErr,
		)
	}

	if result.Claimed != 1 {
		t.Fatalf(
			"claimed = %d, expected 1",
			result.Claimed,
		)
	}

	if result.RetryScheduled != 0 {
		t.Fatalf(
			"retry scheduled = %d, expected 0",
			result.RetryScheduled,
		)
	}
}

package outbox

import (
	"context"
	"encoding/json"
	"time"
)

type Event struct {
	ID              string
	AggregateType   string
	AggregateID     string
	EventType       string
	SchemaVersion   int16
	Payload         json.RawMessage
	OccurredAt      time.Time
	ClaimToken      string
	PublishAttempts int
}

type ClaimPendingInput struct {
	ClaimedAt     time.Time
	LeaseDuration time.Duration
	Limit         int
}

type MarkPublishedInput struct {
	EventID     string
	ClaimToken  string
	PublishedAt time.Time
}

type MarkFailedInput struct {
	EventID      string
	ClaimToken   string
	FailedAt     time.Time
	RetryAt      time.Time
	ErrorMessage string
}

type Store interface {
	ClaimPending(
		ctx context.Context,
		input ClaimPendingInput,
	) ([]Event, error)

	MarkPublished(
		ctx context.Context,
		input MarkPublishedInput,
	) (bool, error)

	MarkFailed(
		ctx context.Context,
		input MarkFailedInput,
	) (bool, error)
}

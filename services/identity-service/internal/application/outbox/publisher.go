package outbox

import (
	"context"
	"encoding/json"
	"time"
)

type Message struct {
	ID            string
	AggregateType string
	AggregateID   string
	EventType     string
	SchemaVersion int16
	Payload       json.RawMessage
	OccurredAt    time.Time
}

type Publisher interface {
	Publish(
		ctx context.Context,
		message Message,
	) error
}

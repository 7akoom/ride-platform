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

// Publisher delivers a claimed outbox event to the message bus (NATS
// JetStream). Wire this the same way identity-service does in
// internal/infrastructure/messaging/nats/jetstream_publisher.go — that
// code is copy-paste reusable across services since the interface matches.
type Publisher interface {
	Publish(
		ctx context.Context,
		message Message,
	) error
}

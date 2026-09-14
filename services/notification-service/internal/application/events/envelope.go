package events

import (
	"encoding/json"
	"time"
)

// Envelope mirrors the JSON shape every outbox publisher wraps its
// message in (see each service's infrastructure/messaging/nats
// jetstream_publisher.go) — decoding it is how a consumer recovers the
// event's own ID (used as the notification's idempotency key) and its
// domain payload.
type Envelope struct {
	EventID       string          `json:"event_id"`
	EventType     string          `json:"event_type"`
	SchemaVersion int16           `json:"schema_version"`
	AggregateType string          `json:"aggregate_type"`
	AggregateID   string          `json:"aggregate_id"`
	OccurredAt    time.Time       `json:"occurred_at"`
	Payload       json.RawMessage `json:"payload"`
}

func Decode(data []byte) (Envelope, error) {
	var envelope Envelope

	if err := json.Unmarshal(data, &envelope); err != nil {
		return Envelope{}, err
	}

	return envelope, nil
}

package ingest

import (
	"encoding/json"
	"time"
)

// Envelope mirrors the JSON shape every outbox publisher wraps its message
// in (see e.g. trip-service's internal/application/outbox/publisher.go and
// its NATS JetStream publisher). Decoding it recovers the event's own ID
// (used as raw_events' idempotency key) alongside its domain-specific
// payload, which each event type unmarshals on its own.
type Envelope struct {
	EventID       string          `json:"event_id"`
	EventType     string          `json:"event_type"`
	SchemaVersion int16           `json:"schema_version"`
	AggregateType string          `json:"aggregate_type"`
	AggregateID   string          `json:"aggregate_id"`
	OccurredAt    time.Time       `json:"occurred_at"`
	Payload       json.RawMessage `json:"payload"`
}

func DecodeEnvelope(data []byte) (Envelope, error) {
	var envelope Envelope

	if err := json.Unmarshal(data, &envelope); err != nil {
		return Envelope{}, err
	}

	return envelope, nil
}

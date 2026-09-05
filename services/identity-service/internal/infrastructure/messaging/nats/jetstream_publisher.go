package nats

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	outboxapp "github.com/7akoom/ride-platform/services/identity-service/internal/application/outbox"
	natsgo "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const eventContentType = "application/json"

type jetStreamMessagePublisher interface {
	PublishMsg(
		ctx context.Context,
		msg *natsgo.Msg,
		opts ...jetstream.PublishOpt,
	) (*jetstream.PubAck, error)
}

type JetStreamPublisher struct {
	publisher      jetStreamMessagePublisher
	publishTimeout time.Duration
}

type eventEnvelope struct {
	EventID       string          `json:"event_id"`
	EventType     string          `json:"event_type"`
	SchemaVersion int16           `json:"schema_version"`
	AggregateType string          `json:"aggregate_type"`
	AggregateID   string          `json:"aggregate_id"`
	OccurredAt    time.Time       `json:"occurred_at"`
	Payload       json.RawMessage `json:"payload"`
}

func NewJetStreamPublisher(
	publisher jetStreamMessagePublisher,
	publishTimeout time.Duration,
) *JetStreamPublisher {
	if publisher == nil {
		panic("JetStream publisher is required")
	}

	if publishTimeout <= 0 {
		panic("JetStream publish timeout must be positive")
	}

	return &JetStreamPublisher{
		publisher:      publisher,
		publishTimeout: publishTimeout,
	}
}

func (p *JetStreamPublisher) Publish(
	ctx context.Context,
	message outboxapp.Message,
) error {
	if err := validateMessage(message); err != nil {
		return err
	}

	envelope := eventEnvelope{
		EventID:       message.ID,
		EventType:     message.EventType,
		SchemaVersion: message.SchemaVersion,
		AggregateType: message.AggregateType,
		AggregateID:   message.AggregateID,
		OccurredAt:    message.OccurredAt.UTC(),
		Payload:       append([]byte(nil), message.Payload...),
	}

	data, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf(
			"marshal outbox event envelope: %w",
			err,
		)
	}

	publishCtx, cancel := context.WithTimeout(
		ctx,
		p.publishTimeout,
	)
	defer cancel()

	natsMessage := &natsgo.Msg{
		Subject: message.EventType,
		Header:  make(natsgo.Header),
		Data:    data,
	}

	natsMessage.Header.Set(
		"Content-Type",
		eventContentType,
	)

	natsMessage.Header.Set(
		jetstream.MsgIDHeader,
		message.ID,
	)

	_, err = p.publisher.PublishMsg(
		publishCtx,
		natsMessage,
	)
	if err != nil {
		return fmt.Errorf(
			"publish outbox event %q to subject %q: %w",
			message.ID,
			message.EventType,
			err,
		)
	}

	return nil
}

func validateMessage(
	message outboxapp.Message,
) error {
	if err := validateRequiredTrimmed(
		"event ID",
		message.ID,
	); err != nil {
		return err
	}

	if err := validateRequiredTrimmed(
		"aggregate type",
		message.AggregateType,
	); err != nil {
		return err
	}

	if err := validateRequiredTrimmed(
		"aggregate ID",
		message.AggregateID,
	); err != nil {
		return err
	}

	if err := validateRequiredTrimmed(
		"event type",
		message.EventType,
	); err != nil {
		return err
	}

	if message.SchemaVersion <= 0 {
		return errors.New(
			"outbox event schema version must be positive",
		)
	}

	if message.OccurredAt.IsZero() {
		return errors.New(
			"outbox event occurrence time cannot be zero",
		)
	}

	if len(message.Payload) == 0 {
		return errors.New(
			"outbox event payload cannot be empty",
		)
	}

	if !json.Valid(message.Payload) {
		return errors.New(
			"outbox event payload must contain valid JSON",
		)
	}

	return nil
}

func validateRequiredTrimmed(
	name string,
	value string,
) error {
	trimmed := strings.TrimSpace(value)

	if trimmed == "" {
		return fmt.Errorf(
			"outbox %s cannot be blank",
			name,
		)
	}

	if value != trimmed {
		return fmt.Errorf(
			"outbox %s must be trimmed",
			name,
		)
	}

	return nil
}

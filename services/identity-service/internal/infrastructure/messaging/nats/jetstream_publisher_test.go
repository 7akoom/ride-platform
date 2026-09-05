package nats

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	outboxapp "github.com/7akoom/ride-platform/services/identity-service/internal/application/outbox"
	natsgo "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type jetStreamPublisherTestFake struct {
	mu sync.Mutex

	calls int

	message *natsgo.Msg
	options []jetstream.PublishOpt

	publishError   error
	waitForContext bool
}

func cloneNATSHeader(
	header natsgo.Header,
) natsgo.Header {
	if header == nil {
		return nil
	}

	cloned := make(
		natsgo.Header,
		len(header),
	)

	for key, values := range header {
		cloned[key] = append(
			[]string(nil),
			values...,
		)
	}

	return cloned
}

func (f *jetStreamPublisherTestFake) PublishMsg(
	ctx context.Context,
	msg *natsgo.Msg,
	opts ...jetstream.PublishOpt,
) (*jetstream.PubAck, error) {
	f.mu.Lock()
	f.calls++

	copiedMessage := &natsgo.Msg{
		Subject: msg.Subject,
		Header: cloneNATSHeader(
			msg.Header,
		),
		Data: append([]byte(nil), msg.Data...),
	}

	f.message = copiedMessage
	f.options = append(
		[]jetstream.PublishOpt(nil),
		opts...,
	)

	waitForContext := f.waitForContext
	publishError := f.publishError

	f.mu.Unlock()

	if waitForContext {
		<-ctx.Done()

		return nil, ctx.Err()
	}

	if publishError != nil {
		return nil, publishError
	}

	return nil, nil
}

func (f *jetStreamPublisherTestFake) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.calls
}

func (f *jetStreamPublisherTestFake) publishedMessage() *natsgo.Msg {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.message == nil {
		return nil
	}

	return &natsgo.Msg{
		Subject: f.message.Subject,
		Header: cloneNATSHeader(
			f.message.Header,
		),
		Data: append([]byte(nil), f.message.Data...),
	}
}

func validJetStreamTestMessage() outboxapp.Message {
	return outboxapp.Message{
		ID:            "11111111-1111-1111-1111-111111111111",
		AggregateType: "identity",
		AggregateID:   "22222222-2222-2222-2222-222222222222",
		EventType:     "identity.suspended",
		SchemaVersion: 1,
		Payload: json.RawMessage(
			`{
				"identity_id":"22222222-2222-2222-2222-222222222222",
				"previous_status":"active",
				"current_status":"suspended"
			}`,
		),
		OccurredAt: time.Date(
			2026,
			time.August,
			28,
			20,
			30,
			0,
			0,
			time.FixedZone(
				"UTC+3",
				3*60*60,
			),
		),
	}
}

func TestJetStreamPublisherPublishesExpectedMessage(
	t *testing.T,
) {
	fake := &jetStreamPublisherTestFake{}

	publisher := NewJetStreamPublisher(
		fake,
		5*time.Second,
	)

	input := validJetStreamTestMessage()

	err := publisher.Publish(
		context.Background(),
		input,
	)
	if err != nil {
		t.Fatalf(
			"Publish() returned an error: %v",
			err,
		)
	}

	if fake.callCount() != 1 {
		t.Fatalf(
			"PublishMsg() calls = %d, expected 1",
			fake.callCount(),
		)
	}

	published := fake.publishedMessage()
	if published == nil {
		t.Fatal(
			"Publish() did not send a NATS message",
		)
	}

	if published.Subject != input.EventType {
		t.Fatalf(
			"Subject = %q, expected %q",
			published.Subject,
			input.EventType,
		)
	}

	if published.Header.Get(
		"Content-Type",
	) != eventContentType {
		t.Fatalf(
			"Content-Type = %q, expected %q",
			published.Header.Get(
				"Content-Type",
			),
			eventContentType,
		)
	}

	if published.Header.Get(
		jetstream.MsgIDHeader,
	) != input.ID {
		t.Fatalf(
			"%s = %q, expected %q",
			jetstream.MsgIDHeader,
			published.Header.Get(
				jetstream.MsgIDHeader,
			),
			input.ID,
		)
	}

	var envelope eventEnvelope

	if err := json.Unmarshal(
		published.Data,
		&envelope,
	); err != nil {
		t.Fatalf(
			"decode published event envelope: %v",
			err,
		)
	}

	if envelope.EventID != input.ID {
		t.Fatalf(
			"EventID = %q, expected %q",
			envelope.EventID,
			input.ID,
		)
	}

	if envelope.EventType != input.EventType {
		t.Fatalf(
			"EventType = %q, expected %q",
			envelope.EventType,
			input.EventType,
		)
	}

	if envelope.SchemaVersion != input.SchemaVersion {
		t.Fatalf(
			"SchemaVersion = %d, expected %d",
			envelope.SchemaVersion,
			input.SchemaVersion,
		)
	}

	if envelope.AggregateType != input.AggregateType {
		t.Fatalf(
			"AggregateType = %q, expected %q",
			envelope.AggregateType,
			input.AggregateType,
		)
	}

	if envelope.AggregateID != input.AggregateID {
		t.Fatalf(
			"AggregateID = %q, expected %q",
			envelope.AggregateID,
			input.AggregateID,
		)
	}

	expectedOccurredAt :=
		input.OccurredAt.UTC()

	if !envelope.OccurredAt.Equal(
		expectedOccurredAt,
	) {
		t.Fatalf(
			"OccurredAt = %v, expected %v",
			envelope.OccurredAt,
			expectedOccurredAt,
		)
	}

	if envelope.OccurredAt.Location() !=
		time.UTC {
		t.Fatalf(
			"OccurredAt location = %v, expected UTC",
			envelope.OccurredAt.Location(),
		)
	}

	var expectedPayload map[string]any
	if err := json.Unmarshal(
		input.Payload,
		&expectedPayload,
	); err != nil {
		t.Fatalf(
			"decode expected payload: %v",
			err,
		)
	}

	var actualPayload map[string]any
	if err := json.Unmarshal(
		envelope.Payload,
		&actualPayload,
	); err != nil {
		t.Fatalf(
			"decode actual payload: %v",
			err,
		)
	}

	expectedJSON, err := json.Marshal(
		expectedPayload,
	)
	if err != nil {
		t.Fatalf(
			"encode expected payload: %v",
			err,
		)
	}

	actualJSON, err := json.Marshal(
		actualPayload,
	)
	if err != nil {
		t.Fatalf(
			"encode actual payload: %v",
			err,
		)
	}

	if string(actualJSON) != string(expectedJSON) {
		t.Fatalf(
			"Payload = %s, expected %s",
			actualJSON,
			expectedJSON,
		)
	}
}

func TestJetStreamPublisherReturnsPublishError(
	t *testing.T,
) {
	publishError := errors.New(
		"JetStream unavailable",
	)

	fake := &jetStreamPublisherTestFake{
		publishError: publishError,
	}

	publisher := NewJetStreamPublisher(
		fake,
		5*time.Second,
	)

	input := validJetStreamTestMessage()

	err := publisher.Publish(
		context.Background(),
		input,
	)
	if err == nil {
		t.Fatal(
			"Publish() returned nil error",
		)
	}

	if !errors.Is(
		err,
		publishError,
	) {
		t.Fatalf(
			"Publish() error = %v, expected wrapped publish error",
			err,
		)
	}

	if !strings.Contains(
		err.Error(),
		input.ID,
	) {
		t.Fatalf(
			"Publish() error does not contain event ID: %v",
			err,
		)
	}

	if !strings.Contains(
		err.Error(),
		input.EventType,
	) {
		t.Fatalf(
			"Publish() error does not contain event type: %v",
			err,
		)
	}
}

func TestJetStreamPublisherAppliesPublishTimeout(
	t *testing.T,
) {
	fake := &jetStreamPublisherTestFake{
		waitForContext: true,
	}

	publisher := NewJetStreamPublisher(
		fake,
		10*time.Millisecond,
	)

	err := publisher.Publish(
		context.Background(),
		validJetStreamTestMessage(),
	)
	if err == nil {
		t.Fatal(
			"Publish() returned nil error after timeout",
		)
	}

	if !errors.Is(
		err,
		context.DeadlineExceeded,
	) {
		t.Fatalf(
			"Publish() error = %v, expected context deadline exceeded",
			err,
		)
	}

	if fake.callCount() != 1 {
		t.Fatalf(
			"PublishMsg() calls = %d, expected 1",
			fake.callCount(),
		)
	}
}

func TestJetStreamPublisherRespectsParentContextCancellation(
	t *testing.T,
) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)

	cancel()

	fake := &jetStreamPublisherTestFake{
		waitForContext: true,
	}

	publisher := NewJetStreamPublisher(
		fake,
		time.Hour,
	)

	err := publisher.Publish(
		ctx,
		validJetStreamTestMessage(),
	)
	if err == nil {
		t.Fatal(
			"Publish() returned nil error for cancelled context",
		)
	}

	if !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf(
			"Publish() error = %v, expected context canceled",
			err,
		)
	}
}

func TestJetStreamPublisherRejectsInvalidMessage(
	t *testing.T,
) {
	tests := []struct {
		name   string
		mutate func(*outboxapp.Message)
	}{
		{
			name: "blank event ID",
			mutate: func(
				message *outboxapp.Message,
			) {
				message.ID = "   "
			},
		},
		{
			name: "untrimmed event ID",
			mutate: func(
				message *outboxapp.Message,
			) {
				message.ID =
					" event-id "
			},
		},
		{
			name: "blank aggregate type",
			mutate: func(
				message *outboxapp.Message,
			) {
				message.AggregateType = ""
			},
		},
		{
			name: "untrimmed aggregate type",
			mutate: func(
				message *outboxapp.Message,
			) {
				message.AggregateType =
					" identity "
			},
		},
		{
			name: "blank aggregate ID",
			mutate: func(
				message *outboxapp.Message,
			) {
				message.AggregateID = ""
			},
		},
		{
			name: "untrimmed aggregate ID",
			mutate: func(
				message *outboxapp.Message,
			) {
				message.AggregateID =
					" identity-id "
			},
		},
		{
			name: "blank event type",
			mutate: func(
				message *outboxapp.Message,
			) {
				message.EventType = ""
			},
		},
		{
			name: "untrimmed event type",
			mutate: func(
				message *outboxapp.Message,
			) {
				message.EventType =
					" identity.suspended "
			},
		},
		{
			name: "zero schema version",
			mutate: func(
				message *outboxapp.Message,
			) {
				message.SchemaVersion = 0
			},
		},
		{
			name: "negative schema version",
			mutate: func(
				message *outboxapp.Message,
			) {
				message.SchemaVersion = -1
			},
		},
		{
			name: "zero occurrence time",
			mutate: func(
				message *outboxapp.Message,
			) {
				message.OccurredAt =
					time.Time{}
			},
		},
		{
			name: "empty payload",
			mutate: func(
				message *outboxapp.Message,
			) {
				message.Payload = nil
			},
		},
		{
			name: "invalid JSON payload",
			mutate: func(
				message *outboxapp.Message,
			) {
				message.Payload =
					json.RawMessage(
						`{"broken":`,
					)
			},
		},
	}

	for _, testCase := range tests {
		t.Run(
			testCase.name,
			func(t *testing.T) {
				fake :=
					&jetStreamPublisherTestFake{}

				publisher :=
					NewJetStreamPublisher(
						fake,
						5*time.Second,
					)

				message :=
					validJetStreamTestMessage()

				testCase.mutate(
					&message,
				)

				err := publisher.Publish(
					context.Background(),
					message,
				)
				if err == nil {
					t.Fatal(
						"Publish() accepted invalid message",
					)
				}

				if fake.callCount() != 0 {
					t.Fatalf(
						"PublishMsg() calls = %d, expected 0",
						fake.callCount(),
					)
				}
			},
		)
	}
}

func TestNewJetStreamPublisherPanicsForInvalidArguments(
	t *testing.T,
) {
	validPublisher :=
		&jetStreamPublisherTestFake{}

	tests := []struct {
		name      string
		publisher jetStreamMessagePublisher
		timeout   time.Duration
	}{
		{
			name:      "nil publisher",
			publisher: nil,
			timeout:   time.Second,
		},
		{
			name:      "zero timeout",
			publisher: validPublisher,
			timeout:   0,
		},
		{
			name:      "negative timeout",
			publisher: validPublisher,
			timeout:   -time.Second,
		},
	}

	for _, testCase := range tests {
		t.Run(
			testCase.name,
			func(t *testing.T) {
				defer func() {
					if recover() == nil {
						t.Fatal(
							"NewJetStreamPublisher() did not panic",
						)
					}
				}()

				NewJetStreamPublisher(
					testCase.publisher,
					testCase.timeout,
				)
			},
		)
	}
}

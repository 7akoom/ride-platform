//go:build integration

package postgres

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	outboxapp "github.com/7akoom/ride-platform/services/identity-service/internal/application/outbox"
	databaseinfra "github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/database"
	natsinfra "github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/messaging/nats"
	"github.com/nats-io/nats.go/jetstream"
)

type identityLifecycleOutboxE2EClock struct {
	now time.Time
}

func (c identityLifecycleOutboxE2EClock) Now() time.Time {
	return c.now
}

type identityLifecycleOutboxE2EEnvelope struct {
	EventID       string    `json:"event_id"`
	EventType     string    `json:"event_type"`
	SchemaVersion int16     `json:"schema_version"`
	AggregateType string    `json:"aggregate_type"`
	AggregateID   string    `json:"aggregate_id"`
	OccurredAt    time.Time `json:"occurred_at"`
	Payload       struct {
		IdentityID     string `json:"identity_id"`
		PreviousStatus string `json:"previous_status"`
		CurrentStatus  string `json:"current_status"`
	} `json:"payload"`
}

func TestIdentityLifecycleOutboxPublishesToJetStream(
	t *testing.T,
) {
	databaseURL := strings.TrimSpace(
		os.Getenv("DATABASE_URL"),
	)
	if databaseURL == "" {
		t.Fatal("DATABASE_URL is required for integration test")
	}

	natsURL := strings.TrimSpace(
		os.Getenv("NATS_URL"),
	)
	if natsURL == "" {
		natsURL = "nats://127.0.0.1:4222"
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		20*time.Second,
	)
	defer cancel()

	pool, err := databaseinfra.NewPostgresPool(
		ctx,
		databaseURL,
	)
	if err != nil {
		t.Fatalf(
			"connect to PostgreSQL: %v",
			err,
		)
	}
	t.Cleanup(pool.Close)

	var identityID string

	err = pool.QueryRow(
		ctx,
		`
			INSERT INTO identities (
				status
			)
			VALUES ($1)
			RETURNING id::text
		`,
		string(auth.IdentityStatusActive),
	).Scan(
		&identityID,
	)
	if err != nil {
		t.Fatalf(
			"create E2E identity: %v",
			err,
		)
	}

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(
			context.Background(),
			5*time.Second,
		)
		defer cleanupCancel()

		_, cleanupErr := pool.Exec(
			cleanupCtx,
			`
				DELETE FROM outbox_events
				WHERE aggregate_type = $1
				  AND aggregate_id = $2::uuid
			`,
			identityOutboxAggregateType,
			identityID,
		)
		if cleanupErr != nil {
			t.Errorf(
				"clean E2E outbox events: %v",
				cleanupErr,
			)
		}

		_, cleanupErr = pool.Exec(
			cleanupCtx,
			`
				DELETE FROM identities
				WHERE id = $1::uuid
			`,
			identityID,
		)
		if cleanupErr != nil {
			t.Errorf(
				"clean E2E identity: %v",
				cleanupErr,
			)
		}
	})

	logger := slog.New(
		slog.NewTextHandler(
			io.Discard,
			nil,
		),
	)

	natsConnection, err := natsinfra.OpenConnection(
		natsinfra.ConnectionConfig{
			URL:            natsURL,
			ClientName:     "identity-outbox-e2e-test",
			ConnectTimeout: 2 * time.Second,
			ReconnectWait:  250 * time.Millisecond,
			DrainTimeout:   2 * time.Second,
		},
		logger,
	)
	if err != nil {
		t.Fatalf(
			"connect to NATS: %v",
			err,
		)
	}

	t.Cleanup(func() {
		if drainErr := natsConnection.Drain(); drainErr != nil {
			t.Errorf(
				"drain NATS connection: %v",
				drainErr,
			)
		}
	})

	stream, err := natsConnection.JetStream().Stream(
		ctx,
		"IDENTITY_EVENTS",
	)
	if err != nil {
		t.Fatalf(
			"get IDENTITY_EVENTS stream: %v",
			err,
		)
	}

	transitionedAt := time.Now().UTC()

	lifecycleStore := NewIdentityLifecycleStore(
		pool,
	)

	transitionResult, found, err := lifecycleStore.Transition(
		ctx,
		auth.IdentityLifecycleTransition{
			IdentityID:     identityID,
			TargetStatus:   auth.IdentityStatusSuspended,
			TransitionedAt: transitionedAt,
		},
	)
	if err != nil {
		t.Fatalf(
			"suspend identity: %v",
			err,
		)
	}

	if !found {
		t.Fatal("identity was not found during lifecycle transition")
	}

	if !transitionResult.Changed {
		t.Fatal("identity lifecycle transition did not report a change")
	}

	if transitionResult.PreviousStatus != auth.IdentityStatusActive {
		t.Fatalf(
			"previous status = %q, expected %q",
			transitionResult.PreviousStatus,
			auth.IdentityStatusActive,
		)
	}

	if transitionResult.CurrentStatus != auth.IdentityStatusSuspended {
		t.Fatalf(
			"current status = %q, expected %q",
			transitionResult.CurrentStatus,
			auth.IdentityStatusSuspended,
		)
	}

	var eventID string
	var eventType string
	var publishedBeforeProcessing bool
	var publishAttemptsBeforeProcessing int

	err = pool.QueryRow(
		ctx,
		`
			SELECT
				id::text,
				event_type,
				published_at IS NOT NULL,
				publish_attempts
			FROM outbox_events
			WHERE aggregate_type = $1
			  AND aggregate_id = $2::uuid
			ORDER BY created_at DESC, id DESC
			LIMIT 1
		`,
		identityOutboxAggregateType,
		identityID,
	).Scan(
		&eventID,
		&eventType,
		&publishedBeforeProcessing,
		&publishAttemptsBeforeProcessing,
	)
	if err != nil {
		t.Fatalf(
			"query lifecycle outbox event before processing: %v",
			err,
		)
	}

	expectedEventType := string(
		auth.IdentityDomainEventSuspended,
	)

	if eventType != expectedEventType {
		t.Fatalf(
			"outbox event type = %q, expected %q",
			eventType,
			expectedEventType,
		)
	}

	if publishedBeforeProcessing {
		t.Fatal(
			"outbox event was already published before processor execution",
		)
	}

	if publishAttemptsBeforeProcessing != 0 {
		t.Fatalf(
			"publish attempts before processing = %d, expected 0",
			publishAttemptsBeforeProcessing,
		)
	}

	outboxStore := NewOutboxStore(
		pool,
	)

	publisher := natsinfra.NewJetStreamPublisher(
		natsConnection.JetStream(),
		2*time.Second,
	)

	processor := outboxapp.NewProcessor(
		outboxStore,
		publisher,
		identityLifecycleOutboxE2EClock{
			now: transitionedAt.Add(time.Second),
		},
		outboxapp.ProcessorConfig{
			BatchSize:         1000,
			LeaseDuration:     30 * time.Second,
			InitialRetryDelay: time.Second,
			MaxRetryDelay:     time.Minute,
		},
	)

	processResult, err := processor.ProcessOnce(
		ctx,
	)
	if err != nil {
		t.Fatalf(
			"process outbox events: %v",
			err,
		)
	}

	if processResult.Published < 1 {
		t.Fatalf(
			"published events = %d, expected at least 1",
			processResult.Published,
		)
	}

	var publishedAfterProcessing bool
	var publishAttemptsAfterProcessing int
	var lastError string

	err = pool.QueryRow(
		ctx,
		`
			SELECT
				published_at IS NOT NULL,
				publish_attempts,
				COALESCE(last_error, '')
			FROM outbox_events
			WHERE id = $1::uuid
		`,
		eventID,
	).Scan(
		&publishedAfterProcessing,
		&publishAttemptsAfterProcessing,
		&lastError,
	)
	if err != nil {
		t.Fatalf(
			"query lifecycle outbox event after processing: %v",
			err,
		)
	}

	if !publishedAfterProcessing {
		t.Fatal(
			"outbox event was not marked as published",
		)
	}

	if publishAttemptsAfterProcessing != 1 {
		t.Fatalf(
			"publish attempts after processing = %d, expected 1",
			publishAttemptsAfterProcessing,
		)
	}

	if lastError != "" {
		t.Fatalf(
			"outbox last error = %q, expected empty",
			lastError,
		)
	}

	rawMessage, err := stream.GetLastMsgForSubject(
		ctx,
		expectedEventType,
	)
	if err != nil {
		t.Fatalf(
			"get published lifecycle event from JetStream: %v",
			err,
		)
	}

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(
			context.Background(),
			5*time.Second,
		)
		defer cleanupCancel()

		if cleanupErr := stream.DeleteMsg(
			cleanupCtx,
			rawMessage.Sequence,
		); cleanupErr != nil {
			t.Errorf(
				"delete E2E JetStream message: %v",
				cleanupErr,
			)
		}
	})

	if rawMessage.Subject != expectedEventType {
		t.Fatalf(
			"JetStream subject = %q, expected %q",
			rawMessage.Subject,
			expectedEventType,
		)
	}

	if rawMessage.Header.Get(
		jetstream.MsgIDHeader,
	) != eventID {
		t.Fatalf(
			"JetStream message ID = %q, expected %q",
			rawMessage.Header.Get(
				jetstream.MsgIDHeader,
			),
			eventID,
		)
	}

	var envelope identityLifecycleOutboxE2EEnvelope

	if err := json.Unmarshal(
		rawMessage.Data,
		&envelope,
	); err != nil {
		t.Fatalf(
			"decode JetStream event envelope: %v",
			err,
		)
	}

	if envelope.EventID != eventID {
		t.Fatalf(
			"envelope event ID = %q, expected %q",
			envelope.EventID,
			eventID,
		)
	}

	if envelope.EventType != expectedEventType {
		t.Fatalf(
			"envelope event type = %q, expected %q",
			envelope.EventType,
			expectedEventType,
		)
	}

	if envelope.SchemaVersion != 1 {
		t.Fatalf(
			"envelope schema version = %d, expected 1",
			envelope.SchemaVersion,
		)
	}

	if envelope.AggregateType != identityOutboxAggregateType {
		t.Fatalf(
			"envelope aggregate type = %q, expected %q",
			envelope.AggregateType,
			identityOutboxAggregateType,
		)
	}

	if envelope.AggregateID != identityID {
		t.Fatalf(
			"envelope aggregate ID = %q, expected %q",
			envelope.AggregateID,
			identityID,
		)
	}

	if envelope.OccurredAt.IsZero() {
		t.Fatal("envelope occurred_at is zero")
	}

	if envelope.Payload.IdentityID != identityID {
		t.Fatalf(
			"payload identity ID = %q, expected %q",
			envelope.Payload.IdentityID,
			identityID,
		)
	}

	if envelope.Payload.PreviousStatus != string(
		auth.IdentityStatusActive,
	) {
		t.Fatalf(
			"payload previous status = %q, expected %q",
			envelope.Payload.PreviousStatus,
			auth.IdentityStatusActive,
		)
	}

	if envelope.Payload.CurrentStatus != string(
		auth.IdentityStatusSuspended,
	) {
		t.Fatalf(
			"payload current status = %q, expected %q",
			envelope.Payload.CurrentStatus,
			auth.IdentityStatusSuspended,
		)
	}
}

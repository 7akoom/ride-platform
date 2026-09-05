package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	"github.com/jackc/pgx/v5"
)

const identityOutboxAggregateType = "identity"

type identityCreatedOutboxPayload struct {
	IdentityID string              `json:"identity_id"`
	Status     auth.IdentityStatus `json:"status"`
}

type identityIdentifierOutboxPayload struct {
	IdentityID     string              `json:"identity_id"`
	IdentifierType auth.IdentifierType `json:"identifier_type"`
}

type identitySessionRevokedOutboxPayload struct {
	IdentityID string `json:"identity_id"`
	SessionID  string `json:"session_id"`
}

type identitySessionsRevokedOutboxPayload struct {
	IdentityID   string   `json:"identity_id"`
	SessionIDs   []string `json:"session_ids"`
	SessionCount int      `json:"session_count"`
}

type identityRefreshTokenReuseDetectedOutboxPayload struct {
	IdentityID string `json:"identity_id"`
	SessionID  string `json:"session_id"`
}

type identityLifecycleOutboxPayload struct {
	IdentityID     string              `json:"identity_id"`
	PreviousStatus auth.IdentityStatus `json:"previous_status"`
	CurrentStatus  auth.IdentityStatus `json:"current_status"`
}

func insertIdentityCreatedOutboxEventInTransaction(
	ctx context.Context,
	tx pgx.Tx,
	event auth.IdentityCreatedDomainEvent,
) error {
	err := insertIdentityOutboxEventInTransaction(
		ctx,
		tx,
		event.IdentityID,
		event.Type,
		event.SchemaVersion,
		identityCreatedOutboxPayload{
			IdentityID: event.IdentityID,
			Status:     event.Status,
		},
		event.OccurredAt,
	)
	if err != nil {
		return fmt.Errorf(
			"insert identity created outbox event: %w",
			err,
		)
	}

	return nil
}

func insertIdentityIdentifierOutboxEventInTransaction(
	ctx context.Context,
	tx pgx.Tx,
	event auth.IdentityIdentifierDomainEvent,
) error {
	err := insertIdentityOutboxEventInTransaction(
		ctx,
		tx,
		event.IdentityID,
		event.Type,
		event.SchemaVersion,
		identityIdentifierOutboxPayload{
			IdentityID:     event.IdentityID,
			IdentifierType: event.IdentifierType,
		},
		event.OccurredAt,
	)
	if err != nil {
		return fmt.Errorf(
			"insert identity identifier outbox event: %w",
			err,
		)
	}

	return nil
}

func insertIdentitySessionRevokedOutboxEventInTransaction(
	ctx context.Context,
	tx pgx.Tx,
	event auth.IdentitySessionRevokedDomainEvent,
) error {
	err := insertIdentityOutboxEventInTransaction(
		ctx,
		tx,
		event.IdentityID,
		event.Type,
		event.SchemaVersion,
		identitySessionRevokedOutboxPayload{
			IdentityID: event.IdentityID,
			SessionID:  event.SessionID,
		},
		event.OccurredAt,
	)
	if err != nil {
		return fmt.Errorf(
			"insert identity session revoked outbox event: %w",
			err,
		)
	}

	return nil
}

func insertIdentitySessionsRevokedOutboxEventInTransaction(
	ctx context.Context,
	tx pgx.Tx,
	event auth.IdentitySessionsRevokedDomainEvent,
) error {
	err := insertIdentityOutboxEventInTransaction(
		ctx,
		tx,
		event.IdentityID,
		event.Type,
		event.SchemaVersion,
		identitySessionsRevokedOutboxPayload{
			IdentityID:   event.IdentityID,
			SessionIDs:   event.SessionIDs,
			SessionCount: len(event.SessionIDs),
		},
		event.OccurredAt,
	)
	if err != nil {
		return fmt.Errorf(
			"insert identity sessions revoked outbox event: %w",
			err,
		)
	}

	return nil
}

func insertIdentityRefreshTokenReuseDetectedOutboxEventInTransaction(
	ctx context.Context,
	tx pgx.Tx,
	event auth.IdentityRefreshTokenReuseDetectedDomainEvent,
) error {
	err := insertIdentityOutboxEventInTransaction(
		ctx,
		tx,
		event.IdentityID,
		event.Type,
		event.SchemaVersion,
		identityRefreshTokenReuseDetectedOutboxPayload{
			IdentityID: event.IdentityID,
			SessionID:  event.SessionID,
		},
		event.OccurredAt,
	)
	if err != nil {
		return fmt.Errorf(
			"insert identity refresh token reuse detected outbox event: %w",
			err,
		)
	}

	return nil
}

func insertIdentityLifecycleOutboxEventInTransaction(
	ctx context.Context,
	tx pgx.Tx,
	event auth.IdentityLifecycleDomainEvent,
) error {
	err := insertIdentityOutboxEventInTransaction(
		ctx,
		tx,
		event.IdentityID,
		event.Type,
		event.SchemaVersion,
		identityLifecycleOutboxPayload{
			IdentityID:     event.IdentityID,
			PreviousStatus: event.PreviousStatus,
			CurrentStatus:  event.CurrentStatus,
		},
		event.OccurredAt,
	)
	if err != nil {
		return fmt.Errorf(
			"insert identity lifecycle outbox event: %w",
			err,
		)
	}

	return nil
}

func insertIdentityOutboxEventInTransaction(
	ctx context.Context,
	tx pgx.Tx,
	identityID string,
	eventType auth.IdentityDomainEventType,
	schemaVersion int16,
	payload any,
	occurredAt time.Time,
) error {
	encodedPayload, err := json.Marshal(
		payload,
	)
	if err != nil {
		return fmt.Errorf(
			"marshal identity outbox payload: %w",
			err,
		)
	}

	const insertOutboxEventQuery = `
		INSERT INTO outbox_events (
			aggregate_type,
			aggregate_id,
			event_type,
			schema_version,
			payload,
			occurred_at,
			available_at
		)
		VALUES (
			$1,
			$2::uuid,
			$3,
			$4,
			$5::jsonb,
			$6,
			$6
		)
	`

	if _, err := tx.Exec(
		ctx,
		insertOutboxEventQuery,
		identityOutboxAggregateType,
		identityID,
		string(eventType),
		schemaVersion,
		encodedPayload,
		occurredAt,
	); err != nil {
		return fmt.Errorf(
			"insert identity outbox event: %w",
			err,
		)
	}

	return nil
}

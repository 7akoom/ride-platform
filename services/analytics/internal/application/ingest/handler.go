package ingest

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/analytics/internal/domain"
	"github.com/7akoom/ride-platform/services/analytics/internal/infrastructure/postgres"
)

// unknownCurrency is the bucket used for trip.settled events whose trip_id
// has no matching trip_fare_currency row yet (fare.calculated arrived out
// of order or was never processed). Rare in practice, but must not crash
// ingestion — a mis-bucketed revenue row is recoverable later; a stuck
// consumer is not.
const unknownCurrency = "UNKNOWN"

// Handler turns a decoded envelope into the corresponding Writer calls,
// all inside one transaction per event. It is the single place that knows
// how each event_type maps to the analytics schema.
type Handler struct {
	writer *postgres.Writer
}

func NewHandler(writer *postgres.Writer) *Handler {
	return &Handler{writer: writer}
}

// Dispatch decodes data as an Envelope and processes it. It is safe to call
// with a redelivered message: InsertRawEvent's idempotency check makes the
// whole method a no-op (after the initial insert attempt) for an event_id
// already recorded.
func (h *Handler) Dispatch(ctx context.Context, _ string, data []byte) error {
	envelope, err := DecodeEnvelope(data)
	if err != nil {
		return fmt.Errorf("decode envelope: %w", err)
	}

	return h.writer.WithTx(ctx, func(tx pgx.Tx) error {
		inserted, err := h.writer.InsertRawEvent(ctx, tx, envelope.EventID, envelope.EventType, envelope.Payload, envelope.OccurredAt)
		if err != nil {
			return err
		}
		if !inserted {
			// Already processed this exact event_id — skip the derived-table
			// updates so a JetStream redelivery never double-counts.
			return nil
		}

		day := truncateToDay(envelope.OccurredAt)
		weekStart := isoWeekStart(envelope.OccurredAt)

		switch envelope.EventType {
		case "trip.requested":
			return h.handleTripRequested(ctx, tx, envelope, day, weekStart)
		case "trip.accepted":
			return h.handleTripAccepted(ctx, tx, envelope, day, weekStart)
		case "trip.started":
			return h.handleTripStarted(ctx, tx, envelope, day)
		case "trip.completed":
			return h.handleTripCompleted(ctx, tx, envelope, day)
		case "trip.cancelled":
			return h.handleTripCancelled(ctx, tx, envelope, day)
		case "fare.calculated":
			return h.handleFareCalculated(ctx, tx, envelope, day)
		case "trip.settled":
			return h.handleTripSettled(ctx, tx, envelope, day)
		case "rider.created":
			return h.handleRiderCreated(ctx, tx, envelope, weekStart)
		case "driver.created":
			return h.handleDriverCreated(ctx, tx, envelope, weekStart)
		default:
			// Unknown/future event type — already stored in raw_events above,
			// nothing more to do. Not an error: new event types shouldn't
			// break ingestion of the ones this handler already understands.
			return nil
		}
	})
}

func (h *Handler) handleTripRequested(ctx context.Context, tx pgx.Tx, env Envelope, day, weekStart time.Time) error {
	var p TripRequestedPayload
	if err := unmarshal(env.Payload, &p); err != nil {
		return err
	}

	if err := h.writer.IncrementFunnelRequested(ctx, tx, day); err != nil {
		return err
	}
	if err := h.writer.SetTripStage(ctx, tx, p.TripID, domain.TripStageRequested, env.OccurredAt); err != nil {
		return err
	}
	if p.RiderID != "" {
		if err := h.writer.RecordRiderActivity(ctx, tx, p.RiderID, weekStart); err != nil {
			return err
		}
	}

	return nil
}

func (h *Handler) handleTripAccepted(ctx context.Context, tx pgx.Tx, env Envelope, day, weekStart time.Time) error {
	var p TripAcceptedPayload
	if err := unmarshal(env.Payload, &p); err != nil {
		return err
	}

	if err := h.writer.IncrementFunnelAccepted(ctx, tx, day); err != nil {
		return err
	}
	if err := h.writer.SetTripStage(ctx, tx, p.TripID, domain.TripStageAccepted, env.OccurredAt); err != nil {
		return err
	}
	if p.DriverID != "" {
		if err := h.writer.RecordDriverActivity(ctx, tx, p.DriverID, weekStart); err != nil {
			return err
		}
	}

	return nil
}

func (h *Handler) handleTripStarted(ctx context.Context, tx pgx.Tx, env Envelope, day time.Time) error {
	var p TripStartedPayload
	if err := unmarshal(env.Payload, &p); err != nil {
		return err
	}

	if err := h.writer.IncrementFunnelStarted(ctx, tx, day); err != nil {
		return err
	}

	return h.writer.SetTripStage(ctx, tx, p.TripID, domain.TripStageStarted, env.OccurredAt)
}

func (h *Handler) handleTripCompleted(ctx context.Context, tx pgx.Tx, env Envelope, day time.Time) error {
	var p TripCompletedPayload
	if err := unmarshal(env.Payload, &p); err != nil {
		return err
	}

	if err := h.writer.IncrementFunnelCompleted(ctx, tx, day); err != nil {
		return err
	}

	return h.writer.SetTripStage(ctx, tx, p.TripID, domain.TripStageCompleted, env.OccurredAt)
}

func (h *Handler) handleTripCancelled(ctx context.Context, tx pgx.Tx, env Envelope, day time.Time) error {
	var p TripCancelledPayload
	if err := unmarshal(env.Payload, &p); err != nil {
		return err
	}

	// trip.cancelled itself carries no memory of which stage the trip was
	// at — look it up from what SetTripStage recorded on the way here.
	stage, found, err := h.writer.GetTripStage(ctx, tx, p.TripID)
	if err != nil {
		return err
	}
	if !found {
		stage = domain.TripStageRequested
	}

	if err := h.writer.RecordCancellation(ctx, tx, p.TripID, stage, env.OccurredAt); err != nil {
		return err
	}

	return h.writer.IncrementFunnelCancelled(ctx, tx, day)
}

func (h *Handler) handleFareCalculated(ctx context.Context, tx pgx.Tx, env Envelope, day time.Time) error {
	var p FareCalculatedPayload
	if err := unmarshal(env.Payload, &p); err != nil {
		return err
	}

	if err := h.writer.RecordTripCurrency(ctx, tx, p.TripID, p.CurrencyCode); err != nil {
		return err
	}

	// Gross revenue is recorded here; commission and the trip-count
	// increment happen at trip.settled instead, since that's the event that
	// actually finalizes a trip financially (a calculated fare can be
	// recalculated before settlement).
	return h.writer.RecordRevenue(ctx, tx, day, p.CurrencyCode, p.Total, decimal.Zero, 0)
}

func (h *Handler) handleTripSettled(ctx context.Context, tx pgx.Tx, env Envelope, day time.Time) error {
	var p TripSettledPayload
	if err := unmarshal(env.Payload, &p); err != nil {
		return err
	}

	currency, found, err := h.writer.GetTripCurrency(ctx, tx, p.TripID)
	if err != nil {
		return err
	}
	if !found {
		currency = unknownCurrency
	}

	commission, err := decimal.NewFromString(p.CommissionAmount)
	if err != nil {
		return fmt.Errorf("parse trip.settled commission_amount %q: %w", p.CommissionAmount, err)
	}

	return h.writer.RecordRevenue(ctx, tx, day, currency, decimal.Zero, commission, 1)
}

func (h *Handler) handleRiderCreated(ctx context.Context, tx pgx.Tx, env Envelope, weekStart time.Time) error {
	var p RiderCreatedPayload
	if err := unmarshal(env.Payload, &p); err != nil {
		return err
	}

	return h.writer.RecordRiderCohort(ctx, tx, p.RiderID, weekStart)
}

func (h *Handler) handleDriverCreated(ctx context.Context, tx pgx.Tx, env Envelope, weekStart time.Time) error {
	var p DriverCreatedPayload
	if err := unmarshal(env.Payload, &p); err != nil {
		return err
	}

	return h.writer.RecordDriverCohort(ctx, tx, p.DriverID, weekStart)
}

func truncateToDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

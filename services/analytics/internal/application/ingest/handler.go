package ingest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/analytics/internal/domain"
	"github.com/7akoom/ride-platform/services/analytics/internal/infrastructure/postgres"
)

// errUnusable is an event that can never be counted (no id, bad JSON).
// It is logged and acknowledged, so it does not come back forever.
var errUnusable = errors.New("unusable event")

// Handler turns one event into fact changes, in one transaction per event.
type Handler struct {
	writer *postgres.Writer
	logger *slog.Logger
}

func NewHandler(writer *postgres.Writer, logger *slog.Logger) *Handler {
	if logger == nil {
		panic("ingest logger is required")
	}

	return &Handler{writer: writer, logger: logger}
}

// Dispatch is safe with redelivered messages: an event id already
// processed changes nothing.
func (h *Handler) Dispatch(ctx context.Context, subject string, data []byte) error {
	envelope, err := DecodeEnvelope(data)
	if err != nil || envelope.EventID == "" || envelope.OccurredAt.IsZero() {
		h.logger.Warn("skipping an event that cannot be read", "subject", subject, "error", err)

		return nil
	}

	return h.writer.WithTx(ctx, func(tx pgx.Tx) error {
		fresh, err := h.writer.MarkProcessed(ctx, tx, envelope.EventID, envelope.EventType, envelope.OccurredAt)
		if err != nil || !fresh {
			return err
		}

		if err := h.apply(ctx, tx, envelope); err != nil {
			if !errors.Is(err, errUnusable) {
				return err
			}

			h.logger.Warn("skipping an event that cannot be counted", "event_type", envelope.EventType, "event_id", envelope.EventID, "error", err)
		}

		return nil
	})
}

func (h *Handler) apply(ctx context.Context, tx pgx.Tx, env Envelope) error {
	at := env.OccurredAt.UTC()

	switch env.EventType {
	case "trip.requested":
		var p TripRequestedPayload
		if err := unmarshal(env.Payload, &p); err != nil || p.TripID == "" {
			return payloadError(env, err)
		}

		return h.writer.TripRequested(ctx, tx, postgres.TripRequested{
			TripID: p.TripID, RiderID: p.RiderID, CityID: p.CityID, ZoneID: p.ZoneID,
			VehicleClass: p.VehicleClass, PaymentMethod: p.PaymentMethod, Scheduled: optionalBool(p.Scheduled), At: at,
		})

	case "trip.accepted":
		var p TripAcceptedPayload
		if err := unmarshal(env.Payload, &p); err != nil || p.TripID == "" {
			return payloadError(env, err)
		}

		return h.writer.TripAccepted(ctx, tx, p.TripID, p.DriverID, at)

	case "trip.driver_arrived", "trip.started":
		var p TripIDPayload
		if err := unmarshal(env.Payload, &p); err != nil || p.TripID == "" {
			return payloadError(env, err)
		}

		if env.EventType == "trip.started" {
			return h.writer.TripStarted(ctx, tx, p.TripID, at)
		}

		return h.writer.TripArrived(ctx, tx, p.TripID, at)

	case "trip.completed":
		var p TripCompletedPayload
		if err := unmarshal(env.Payload, &p); err != nil || p.TripID == "" {
			return payloadError(env, err)
		}

		return h.writer.TripCompleted(ctx, tx, p.TripID, p.RiderID, p.DriverID, at)

	case "trip.cancelled":
		var p TripCancelledPayload
		if err := unmarshal(env.Payload, &p); err != nil || p.TripID == "" {
			return payloadError(env, err)
		}

		return h.writer.TripCancelled(ctx, tx, postgres.TripCancelled{
			TripID: p.TripID, CancelledBy: p.CancelledBy, RiderNoShow: p.RiderNoShow == "true",
			Stage: string(CancelStageOf(p.FromStatus, p.DriverArrived == "true")), At: at,
		})

	case "fare.calculated":
		var p FareCalculatedPayload
		if err := unmarshal(env.Payload, &p); err != nil || p.TripID == "" {
			return payloadError(env, err)
		}

		kind := strings.TrimSpace(p.Kind)
		if kind == "" {
			kind = domain.FareKindTrip
		}

		return h.writer.FareCalculated(ctx, tx, postgres.Fare{
			TripID: p.TripID, RiderID: p.RiderID, Kind: kind, Currency: p.CurrencyCode, Total: p.Total, At: at,
		})

	case "trip.settled":
		var p TripSettledPayload
		if err := unmarshal(env.Payload, &p); err != nil || p.TripID == "" {
			return payloadError(env, err)
		}

		commission, err := decimal.NewFromString(p.CommissionAmount)
		if err != nil {
			return payloadError(env, err)
		}

		return h.writer.TripSettled(ctx, tx, p.TripID, p.DriverID, commission, at)

	case "rider.created":
		var p RiderCreatedPayload
		if err := unmarshal(env.Payload, &p); err != nil || p.RiderID == "" {
			return payloadError(env, err)
		}

		return h.writer.RiderSignedUp(ctx, tx, p.RiderID, at)

	case "driver.created", "driver.approved":
		var p DriverPayload
		if err := unmarshal(env.Payload, &p); err != nil || p.DriverID == "" {
			return payloadError(env, err)
		}

		if env.EventType == "driver.approved" {
			return h.writer.DriverApproved(ctx, tx, p.DriverID, at)
		}

		return h.writer.DriverSignedUp(ctx, tx, p.DriverID, at)
	}

	// Other event types are not counted yet.
	return nil
}

// CancelStageOf maps the trip's status when it was cancelled to a stage;
// empty when the event did not say (older events).
func CancelStageOf(fromStatus string, driverArrived bool) domain.CancelStage {
	switch fromStatus {
	case "requested":
		return domain.CancelStageRequested
	case "accepted":
		if driverArrived {
			return domain.CancelStageArrived
		}

		return domain.CancelStageAccepted
	case "in_progress":
		return domain.CancelStageStarted
	}

	return ""
}

func optionalBool(value string) *bool {
	switch value {
	case "true":
		v := true
		return &v
	case "false":
		v := false
		return &v
	}

	return nil
}

func payloadError(env Envelope, err error) error {
	if err == nil {
		err = errors.New("missing id")
	}

	return fmt.Errorf("%w: %s event %s: %v", errUnusable, env.EventType, env.EventID, err)
}

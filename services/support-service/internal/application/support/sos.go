package support

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// SOSAlert is what trip-service publishes when someone presses SOS.
type SOSAlert struct {
	AlertID     string
	TripID      string
	TriggeredBy Audience
	Latitude    string
	Longitude   string
}

// ErrDuplicateSOS: a ticket for the alert exists already.
var ErrDuplicateSOS = errors.New("a ticket for this SOS exists already")

// OpenSOSTicket opens an urgent safety ticket for an SOS, once per alert,
// in the name of whoever pressed it.
func (s *Service) OpenSOSTicket(ctx context.Context, alert SOSAlert) error {
	if !validID(alert.AlertID) || !validID(alert.TripID) || !alert.TriggeredBy.Valid() {
		s.logger.WarnContext(ctx, "unusable SOS event; skipping", "alert_id", alert.AlertID)

		return nil
	}

	if _, found, err := s.repository.FindBySOSAlert(ctx, alert.AlertID); err != nil || found {
		return err
	}

	trip, err := s.trips.GetTrip(ctx, alert.TripID)
	if errors.Is(err, ErrNotFound) {
		s.logger.WarnContext(ctx, "SOS for a trip that does not exist; skipping", "alert_id", alert.AlertID)

		return nil
	}

	if err != nil {
		return err
	}

	profileID := trip.RiderID
	if alert.TriggeredBy == AudienceDriver {
		profileID = trip.DriverID
	}

	if profileID == "" {
		s.logger.WarnContext(ctx, "SOS by a side the trip does not have; skipping", "alert_id", alert.AlertID)

		return nil
	}

	identityID, err := s.profiles.IdentityByProfile(ctx, alert.TriggeredBy, profileID)
	if err != nil {
		return err
	}

	now := s.clock.Now()
	ticket := Ticket{
		ID:                   s.ids.NewID(),
		RequesterIdentityID:  identityID,
		Audience:             alert.TriggeredBy,
		RequesterProfileID:   profileID,
		CategoryKey:          CategorySOS,
		Status:               StatusOpen,
		Priority:             PriorityUrgent,
		Safety:               true,
		Source:               SourceSOS,
		TripID:               trip.ID,
		CounterpartProfileID: counterpartOf(alert.TriggeredBy, trip),
		SOSAlertID:           alert.AlertID,
		CreatedAt:            now,
		UpdatedAt:            now,
		LastMessageAt:        now,
	}

	message := s.systemMessage(ticket.ID, fmt.Sprintf(
		"SOS pressed by the %s during trip %s (trip status %s) at %s,%s",
		alert.TriggeredBy, trip.ID, trip.Status, alert.Latitude, alert.Longitude,
	))

	_, err = s.repository.CreateTicket(ctx, NewTicket{
		Ticket:      ticket,
		Message:     message,
		BuildEvents: func(t Ticket) []Event { return []Event{ticketCreatedEvent(t)} },
	})
	if errors.Is(err, ErrDuplicateSOS) {
		return nil
	}

	return err
}

// SubjectSOSTriggered is published by trip-service when someone presses SOS.
const SubjectSOSTriggered = "trip.sos_triggered"

type eventEnvelope struct {
	EventID string          `json:"event_id"`
	Payload json.RawMessage `json:"payload"`
}

type sosPayload struct {
	TripID      string `json:"trip_id"`
	AlertID     string `json:"alert_id"`
	TriggeredBy string `json:"triggered_by"`
	Latitude    string `json:"latitude"`
	Longitude   string `json:"longitude"`
}

// HandleSOSEvent is the durable consumer's handler for trip.sos_triggered.
// An error redelivers the event; one that cannot be read is dropped.
func (s *Service) HandleSOSEvent(ctx context.Context, subject string, data []byte) error {
	if subject != SubjectSOSTriggered {
		return nil
	}

	var envelope eventEnvelope
	var payload sosPayload

	if err := json.Unmarshal(data, &envelope); err != nil || json.Unmarshal(envelope.Payload, &payload) != nil {
		s.logger.WarnContext(ctx, "unreadable trip.sos_triggered event; skipping")

		return nil
	}

	return s.OpenSOSTicket(ctx, SOSAlert{
		AlertID:     payload.AlertID,
		TripID:      payload.TripID,
		TriggeredBy: Audience(payload.TriggeredBy),
		Latitude:    payload.Latitude,
		Longitude:   payload.Longitude,
	})
}

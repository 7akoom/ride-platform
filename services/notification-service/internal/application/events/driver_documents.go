package events

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/7akoom/ride-platform/services/notification-service/internal/application/notification"
)

// Subjects driver-service publishes about a driver's account and documents.
const (
	SubjectDriverApproved         = "driver.approved"
	SubjectDriverRejected         = "driver.rejected"
	SubjectDriverDocumentReviewed = "driver.document_reviewed"
	SubjectDriverDocumentExpiring = "driver.document_expiring"
	SubjectDriverDocumentExpired  = "driver.document_expired"
	SubjectDriverVehicleReviewed  = "driver.vehicle_reviewed"
)

// DriverSubjects are the driver events that notify the driver.
var DriverSubjects = []string{
	SubjectDriverApproved,
	SubjectDriverRejected,
	SubjectDriverDocumentReviewed,
	SubjectDriverDocumentExpiring,
	SubjectDriverDocumentExpired,
	SubjectDriverVehicleReviewed,
}

type driverStatusPayload struct {
	DriverID string `json:"driver_id"`
	Reason   string `json:"reason"`
}

// driverDocumentPayload carries the document's name in each language, so
// every translation can name it in its own.
type driverDocumentPayload struct {
	DriverID  string `json:"driver_id"`
	TypeCode  string `json:"type_code"`
	NameEN    string `json:"document_name_en"`
	NameAR    string `json:"document_name_ar"`
	NameKU    string `json:"document_name_ku"`
	Decision  string `json:"decision"`
	Reason    string `json:"reason"`
	Withdrawn bool   `json:"withdrawn"`
	ExpiresOn string `json:"expires_on"`
	DaysLeft  int    `json:"days_left"`
}

func (h *Handler) handleDriverStatus(ctx context.Context, subject string, envelope Envelope) error {
	var payload driverStatusPayload

	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		return fmt.Errorf("decode %s payload: %w", subject, err)
	}

	if payload.DriverID == "" {
		h.logger.WarnContext(ctx, "driver status event without a driver; skipping", "subject", subject)

		return nil
	}

	eventKey, variables := "driver.account_approved", map[string]string{}

	if subject == SubjectDriverRejected {
		eventKey = "driver.account_rejected"
		variables["reason"] = payload.Reason
	}

	return h.sendToDriver(ctx, payload.DriverID, eventKey, variables, envelope.EventID)
}

func (h *Handler) handleDriverDocument(ctx context.Context, subject string, envelope Envelope) error {
	var payload driverDocumentPayload

	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		return fmt.Errorf("decode %s payload: %w", subject, err)
	}

	if payload.DriverID == "" {
		h.logger.WarnContext(ctx, "driver document event without a driver; skipping", "subject", subject)

		return nil
	}

	variables := map[string]string{
		"document_en": fallback(payload.NameEN, payload.TypeCode),
		"document_ar": fallback(payload.NameAR, payload.TypeCode),
		"document_ku": fallback(payload.NameKU, payload.TypeCode),
		"date":        payload.ExpiresOn,
	}

	var eventKey string

	switch subject {
	case SubjectDriverDocumentReviewed:
		switch {
		case payload.Decision == "approved":
			eventKey = "driver.document_approved"
		case payload.Withdrawn:
			eventKey = "driver.document_withdrawn"
			variables["reason"] = payload.Reason
		default:
			eventKey = "driver.document_rejected"
			variables["reason"] = payload.Reason
		}
	case SubjectDriverDocumentExpiring:
		eventKey = "driver.document_expiring"
		variables["days"] = fmt.Sprint(payload.DaysLeft)
	default:
		eventKey = "driver.document_expired"
	}

	return h.sendToDriver(ctx, payload.DriverID, eventKey, variables, envelope.EventID)
}

type driverVehiclePayload struct {
	DriverID    string `json:"driver_id"`
	PlateNumber string `json:"plate_number"`
	Make        string `json:"make"`
	Model       string `json:"model"`
	Decision    string `json:"decision"`
	Reason      string `json:"reason"`
}

func (h *Handler) handleDriverVehicle(ctx context.Context, envelope Envelope) error {
	var payload driverVehiclePayload

	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		return fmt.Errorf("decode %s payload: %w", SubjectDriverVehicleReviewed, err)
	}

	if payload.DriverID == "" {
		h.logger.WarnContext(ctx, "vehicle event without a driver; skipping")

		return nil
	}

	variables := map[string]string{
		"car":   strings.TrimSpace(payload.Make + " " + payload.Model),
		"plate": payload.PlateNumber,
	}

	eventKey := "driver.vehicle_approved"
	if payload.Decision != "approved" {
		eventKey = "driver.vehicle_rejected"
		variables["reason"] = payload.Reason
	}

	return h.sendToDriver(ctx, payload.DriverID, eventKey, variables, envelope.EventID)
}

func (h *Handler) sendToDriver(ctx context.Context, driverID, eventKey string, variables map[string]string, idempotencyKey string) error {
	if _, err := h.notificationService.Send(ctx, notification.SendInput{
		RecipientType:  notification.RecipientDriver,
		RecipientID:    driverID,
		EventKey:       eventKey,
		Variables:      variables,
		IdempotencyKey: idempotencyKey,
	}); err != nil {
		return fmt.Errorf("send %s notification: %w", eventKey, err)
	}

	return nil
}

func fallback(value, otherwise string) string {
	if value == "" {
		return otherwise
	}

	return value
}

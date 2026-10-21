package events

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/7akoom/ride-platform/services/notification-service/internal/application/notification"
)

// Subjects pricing-service publishes about offers for riders.
const (
	// SubjectLoyaltyDue: the rider's next completed trip gets the loyalty discount.
	SubjectLoyaltyDue = "fare.offer.loyalty_due"
	// SubjectFeaturedOffer: a featured coupon started running.
	SubjectFeaturedOffer = "fare.offer.featured"
)

// OfferSubjects are read by the notification-offers consumer.
var OfferSubjects = []string{SubjectLoyaltyDue, SubjectFeaturedOffer}

// featuredOfferPage is how many riders one page of an offer for everyone reaches.
const featuredOfferPage = 200

// RiderAudience lists the riders with a phone registered for notifications, by
// id, after the given one (empty: from the start).
type RiderAudience interface {
	RidersWithDevices(ctx context.Context, afterID string, limit int) ([]string, error)
}

// WithRiderAudience lets offers for every rider go out; without it they are
// skipped.
func WithRiderAudience(riders RiderAudience) Option {
	return func(h *Handler) {
		h.riders = riders
	}
}

type loyaltyDuePayload struct {
	RiderID   string `json:"rider_id"`
	Percent   string `json:"percent"`
	MaxAmount string `json:"max_amount"`
}

func (h *Handler) handleLoyaltyDue(ctx context.Context, envelope Envelope) error {
	var payload loyaltyDuePayload

	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		return fmt.Errorf("decode %s payload: %w", SubjectLoyaltyDue, err)
	}

	if payload.RiderID == "" || payload.Percent == "" {
		h.logger.WarnContext(ctx, "loyalty offer without a rider or a percent; skipping", "event_id", envelope.EventID)

		return nil
	}

	if _, err := h.notificationService.Send(ctx, notification.SendInput{
		RecipientType:  notification.RecipientRider,
		RecipientID:    payload.RiderID,
		EventKey:       "promo.loyalty_due",
		Variables:      map[string]string{"percent": payload.Percent},
		IdempotencyKey: envelope.EventID,
	}); err != nil {
		return fmt.Errorf("send promo.loyalty_due notification: %w", err)
	}

	return nil
}

type featuredOfferPayload struct {
	CouponCode  string `json:"coupon_code"`
	Description string `json:"description"`
}

// handleFeaturedOffer tells every rider with a registered phone of a new featured
// coupon. Each rider's notification is keyed by the event and the rider, so a
// redelivery after a partial run reaches only those not told yet.
func (h *Handler) handleFeaturedOffer(ctx context.Context, envelope Envelope) error {
	var payload featuredOfferPayload

	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		return fmt.Errorf("decode %s payload: %w", SubjectFeaturedOffer, err)
	}

	if payload.CouponCode == "" || h.riders == nil {
		return nil
	}

	description := payload.Description
	if description == "" {
		description = payload.CouponCode
	}

	after, told := "", 0

	for {
		riders, err := h.riders.RidersWithDevices(ctx, after, featuredOfferPage)
		if err != nil {
			return fmt.Errorf("list riders for an offer: %w", err)
		}

		for _, rider := range riders {
			if _, err := h.notificationService.Send(ctx, notification.SendInput{
				RecipientType:  notification.RecipientRider,
				RecipientID:    rider,
				EventKey:       "promo.featured_coupon",
				Variables:      map[string]string{"code": payload.CouponCode, "description": description},
				Data:           map[string]string{"coupon_code": payload.CouponCode},
				IdempotencyKey: envelope.EventID + ":" + rider,
			}); err != nil {
				return fmt.Errorf("send promo.featured_coupon notification: %w", err)
			}

			told++
		}

		if len(riders) < featuredOfferPage {
			break
		}

		after = riders[len(riders)-1]
	}

	h.logger.InfoContext(ctx, "riders told of a featured coupon", "coupon_code", payload.CouponCode, "riders", told)

	return nil
}

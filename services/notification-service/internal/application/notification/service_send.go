package notification

import (
	"context"
	"fmt"
	"strings"
)

// Send renders a template for the recipient's language and fans it out
// across the requested channels.
//
// The ordering matters: the in-app record is written FIRST and its
// success is what the caller gets back. Push and SMS are attempted
// after, and their failures are recorded as delivery rows rather than
// returned as errors. A notification the user can see in the app is a
// delivered notification even if Google's push service was down — and
// failing the whole call would tempt callers into retrying, producing
// duplicates.
func (s *service) Send(
	ctx context.Context,
	input SendInput,
) (SendResult, error) {
	if !input.RecipientType.Valid() {
		return SendResult{}, ErrInvalidRecipientType
	}

	recipientID := strings.TrimSpace(input.RecipientID)
	if recipientID == "" {
		return SendResult{}, ErrRecipientIDRequired
	}

	eventKey := strings.TrimSpace(input.EventKey)
	if eventKey == "" {
		return SendResult{}, ErrEventKeyRequired
	}

	idempotencyKey := strings.TrimSpace(input.IdempotencyKey)

	if idempotencyKey != "" {
		existing, deliveries, found, err := s.repository.FindNotificationByIdempotencyKey(ctx, idempotencyKey)
		if err != nil {
			return SendResult{}, fmt.Errorf("check idempotency key: %w", err)
		}

		if found {
			return SendResult{Notification: existing, Deliveries: deliveries}, nil
		}
	}

	template, err := s.repository.FindTemplate(ctx, eventKey)
	if err != nil {
		return SendResult{}, fmt.Errorf("find template: %w", err)
	}

	devices, err := s.repository.ListDevices(ctx, input.RecipientType, recipientID)
	if err != nil {
		return SendResult{}, fmt.Errorf("list devices: %w", err)
	}

	translation, err := s.repository.FindTranslation(ctx, eventKey, localeChain(input.Locale, devices))
	if err != nil {
		return SendResult{}, fmt.Errorf("find translation: %w", err)
	}

	title := Render(translation.Title, input.Variables)
	body := Render(translation.Body, input.Variables)

	channels := input.Channels
	if len(channels) == 0 {
		channels = template.DefaultChannels
	}

	deliveries := s.deliver(ctx, channels, devices, input, title, body)

	persisted, err := s.repository.Persist(ctx, PersistInput{
		RecipientType:  input.RecipientType,
		RecipientID:    recipientID,
		EventKey:       eventKey,
		Title:          title,
		Body:           body,
		Locale:         translation.Locale,
		Data:           input.Data,
		IdempotencyKey: idempotencyKey,
		Deliveries:     deliveries,
	})
	if err != nil {
		return SendResult{}, fmt.Errorf("persist notification: %w", err)
	}

	return SendResult{Notification: persisted, Deliveries: deliveries}, nil
}

// deliver attempts each requested channel and records the outcome. It
// never returns an error: a channel failing is data, not a fault.
func (s *service) deliver(
	ctx context.Context,
	channels []Channel,
	devices []Device,
	input SendInput,
	title, body string,
) []Delivery {
	deliveries := make([]Delivery, 0, len(channels))

	for _, channel := range channels {
		switch channel {
		case ChannelInApp:
			// Satisfied by the notification row itself, written by the
			// caller immediately after this returns.
			deliveries = append(deliveries, Delivery{
				Channel: ChannelInApp,
				Status:  StatusSent,
			})

		case ChannelPush:
			deliveries = append(deliveries, s.deliverPush(ctx, devices, input, title, body))

		case ChannelSMS:
			delivery := Delivery{Channel: ChannelSMS, Status: StatusSent}

			if err := s.smsSender.Send(ctx, input.RecipientType, input.RecipientID, body); err != nil {
				delivery.Status = StatusFailed
				delivery.Detail = err.Error()
			}

			deliveries = append(deliveries, delivery)
		}
	}

	return deliveries
}

func (s *service) deliverPush(
	ctx context.Context,
	devices []Device,
	input SendInput,
	title, body string,
) Delivery {
	if len(devices) == 0 {
		return Delivery{
			Channel: ChannelPush,
			Status:  StatusSkipped,
			Detail:  "recipient has no registered devices",
		}
	}

	result, err := s.pushSender.Send(ctx, devices, title, body, input.Data)
	if err != nil {
		return Delivery{
			Channel: ChannelPush,
			Status:  StatusFailed,
			Detail:  err.Error(),
		}
	}

	// Prune tokens the provider rejected as permanently invalid.
	// Best-effort: failing to clean up must not fail the send.
	if len(result.InvalidTokens) > 0 {
		_ = s.repository.RemoveDeviceTokens(ctx, result.InvalidTokens)
	}

	if result.SentCount == 0 {
		return Delivery{
			Channel: ChannelPush,
			Status:  StatusFailed,
			Detail:  result.Detail,
		}
	}

	return Delivery{
		Channel: ChannelPush,
		Status:  StatusSent,
		Detail:  result.Detail,
	}
}

// localeChain builds the preference order for picking a translation:
// an explicit request wins, then whatever the recipient's most recent
// device is set to, then the default. Duplicates are dropped so the
// repository doesn't query the same locale twice.
func localeChain(requested string, devices []Device) []string {
	chain := make([]string, 0, 3)
	seen := make(map[string]bool, 3)

	add := func(locale string) {
		locale = strings.TrimSpace(locale)

		if locale == "" || seen[locale] {
			return
		}

		seen[locale] = true
		chain = append(chain, locale)
	}

	add(requested)

	for _, device := range devices {
		add(device.Locale)
	}

	add(DefaultLocale)

	return chain
}

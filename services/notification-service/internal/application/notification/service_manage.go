package notification

import (
	"context"
	"fmt"
	"strings"
)

const defaultListLimit = 50
const maxListLimit = 200

func (s *service) RegisterDevice(
	ctx context.Context,
	input RegisterDeviceInput,
) (string, error) {
	if !input.RecipientType.Valid() {
		return "", ErrInvalidRecipientType
	}

	recipientID := strings.TrimSpace(input.RecipientID)
	if recipientID == "" {
		return "", ErrRecipientIDRequired
	}

	deviceToken := strings.TrimSpace(input.DeviceToken)
	if deviceToken == "" {
		return "", ErrDeviceTokenRequired
	}

	if !input.Platform.Valid() {
		return "", ErrInvalidPlatform
	}

	locale := strings.TrimSpace(input.Locale)
	if locale == "" {
		locale = DefaultLocale
	}

	deviceID, err := s.repository.RegisterDevice(ctx, RegisterDeviceInput{
		RecipientType: input.RecipientType,
		RecipientID:   recipientID,
		DeviceToken:   deviceToken,
		Platform:      input.Platform,
		Locale:        locale,
	})
	if err != nil {
		return "", fmt.Errorf("register device: %w", err)
	}

	return deviceID, nil
}

func (s *service) UnregisterDevice(
	ctx context.Context,
	deviceToken string,
) (bool, error) {
	trimmed := strings.TrimSpace(deviceToken)
	if trimmed == "" {
		return false, ErrDeviceTokenRequired
	}

	removed, err := s.repository.UnregisterDevice(ctx, trimmed)
	if err != nil {
		return false, fmt.Errorf("unregister device: %w", err)
	}

	return removed, nil
}

func (s *service) List(
	ctx context.Context,
	input ListInput,
) (ListResult, error) {
	if !input.RecipientType.Valid() {
		return ListResult{}, ErrInvalidRecipientType
	}

	recipientID := strings.TrimSpace(input.RecipientID)
	if recipientID == "" {
		return ListResult{}, ErrRecipientIDRequired
	}

	limit := input.Limit
	if limit <= 0 {
		limit = defaultListLimit
	}

	if limit > maxListLimit {
		limit = maxListLimit
	}

	notifications, err := s.repository.List(ctx, ListInput{
		RecipientType: input.RecipientType,
		RecipientID:   recipientID,
		Limit:         limit,
		UnreadOnly:    input.UnreadOnly,
	})
	if err != nil {
		return ListResult{}, fmt.Errorf("list notifications: %w", err)
	}

	// Returned alongside the list so a client can render a badge count
	// without a second call — the common case for an inbox screen.
	unreadCount, err := s.repository.UnreadCount(ctx, input.RecipientType, recipientID)
	if err != nil {
		return ListResult{}, fmt.Errorf("count unread notifications: %w", err)
	}

	return ListResult{Notifications: notifications, UnreadCount: unreadCount}, nil
}

func (s *service) MarkAsRead(
	ctx context.Context,
	recipientType RecipientType,
	recipientID string,
	ids []string,
) (int, error) {
	if !recipientType.Valid() {
		return 0, ErrInvalidRecipientType
	}

	trimmedID := strings.TrimSpace(recipientID)
	if trimmedID == "" {
		return 0, ErrRecipientIDRequired
	}

	marked, err := s.repository.MarkAsRead(ctx, recipientType, trimmedID, ids)
	if err != nil {
		return 0, fmt.Errorf("mark notifications as read: %w", err)
	}

	return marked, nil
}

func (s *service) UpsertTemplate(
	ctx context.Context,
	input UpsertTemplateInput,
) (int, error) {
	eventKey := strings.TrimSpace(input.EventKey)
	if eventKey == "" {
		return 0, ErrEventKeyRequired
	}

	if len(input.Translations) == 0 {
		return 0, ErrTranslationsRequired
	}

	channels := input.DefaultChannels
	if len(channels) == 0 {
		channels = []Channel{ChannelInApp, ChannelPush}
	}

	for _, channel := range channels {
		if !channel.Valid() {
			return 0, ErrInvalidChannel
		}
	}

	count, err := s.repository.UpsertTemplate(ctx, UpsertTemplateInput{
		EventKey:        eventKey,
		DefaultChannels: channels,
		Translations:    input.Translations,
	})
	if err != nil {
		return 0, fmt.Errorf("upsert template: %w", err)
	}

	return count, nil
}

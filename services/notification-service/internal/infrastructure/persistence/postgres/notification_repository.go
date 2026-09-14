package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/7akoom/ride-platform/services/notification-service/internal/application/notification"
)

type NotificationRepository struct {
	pool *pgxpool.Pool
}

func NewNotificationRepository(pool *pgxpool.Pool) *NotificationRepository {
	if pool == nil {
		panic("postgres pool is required")
	}

	return &NotificationRepository{pool: pool}
}

func (r *NotificationRepository) FindTemplate(
	ctx context.Context,
	eventKey string,
) (notification.Template, error) {
	var template notification.Template
	var channels []string

	err := r.pool.QueryRow(
		ctx,
		`SELECT event_key, default_channels
		 FROM notification_templates
		 WHERE event_key = $1`,
		eventKey,
	).Scan(&template.EventKey, &channels)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return notification.Template{}, notification.ErrTemplateNotFound
		}

		return notification.Template{}, fmt.Errorf("select template: %w", err)
	}

	template.DefaultChannels = make([]notification.Channel, len(channels))

	for i, channel := range channels {
		template.DefaultChannels[i] = notification.Channel(channel)
	}

	return template, nil
}

// FindTranslation resolves the locale preference chain in a single
// query: array_position orders the matches by the caller's preference,
// so the best available translation comes back first.
func (r *NotificationRepository) FindTranslation(
	ctx context.Context,
	eventKey string,
	locales []string,
) (notification.TemplateTranslation, error) {
	var translation notification.TemplateTranslation

	err := r.pool.QueryRow(
		ctx,
		`SELECT event_key, locale, title, body
		 FROM notification_template_translations
		 WHERE event_key = $1 AND locale = ANY($2)
		 ORDER BY array_position($2, locale)
		 LIMIT 1`,
		eventKey,
		locales,
	).Scan(
		&translation.EventKey,
		&translation.Locale,
		&translation.Title,
		&translation.Body,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return notification.TemplateTranslation{}, notification.ErrTranslationNotFound
		}

		return notification.TemplateTranslation{}, fmt.Errorf("select translation: %w", err)
	}

	return translation, nil
}

func (r *NotificationRepository) UpsertTemplate(
	ctx context.Context,
	input notification.UpsertTemplateInput,
) (int, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	channels := make([]string, len(input.DefaultChannels))

	for i, channel := range input.DefaultChannels {
		channels[i] = string(channel)
	}

	if _, err := tx.Exec(
		ctx,
		`INSERT INTO notification_templates (event_key, default_channels)
		 VALUES ($1, $2)
		 ON CONFLICT (event_key) DO UPDATE
		 SET default_channels = EXCLUDED.default_channels,
		     updated_at = CURRENT_TIMESTAMP`,
		input.EventKey,
		channels,
	); err != nil {
		return 0, fmt.Errorf("upsert template: %w", err)
	}

	for _, translation := range input.Translations {
		if _, err := tx.Exec(
			ctx,
			`INSERT INTO notification_template_translations
			    (event_key, locale, title, body)
			 VALUES ($1, $2, $3, $4)
			 ON CONFLICT (event_key, locale) DO UPDATE
			 SET title = EXCLUDED.title,
			     body = EXCLUDED.body,
			     updated_at = CURRENT_TIMESTAMP`,
			input.EventKey,
			translation.Locale,
			translation.Title,
			translation.Body,
		); err != nil {
			return 0, fmt.Errorf("upsert translation: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit transaction: %w", err)
	}

	return len(input.Translations), nil
}

// RegisterDevice reassigns an existing token to the new owner rather
// than rejecting it. The same handset genuinely changes hands — a
// driver signs out, another signs in — and notifications must follow
// the account, not the device's history.
func (r *NotificationRepository) RegisterDevice(
	ctx context.Context,
	input notification.RegisterDeviceInput,
) (string, error) {
	var deviceID string

	err := r.pool.QueryRow(
		ctx,
		`INSERT INTO devices (recipient_type, recipient_id, device_token, platform, locale)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (device_token) DO UPDATE
		 SET recipient_type = EXCLUDED.recipient_type,
		     recipient_id = EXCLUDED.recipient_id,
		     platform = EXCLUDED.platform,
		     locale = EXCLUDED.locale,
		     last_seen_at = CURRENT_TIMESTAMP
		 RETURNING id`,
		string(input.RecipientType),
		input.RecipientID,
		input.DeviceToken,
		string(input.Platform),
		input.Locale,
	).Scan(&deviceID)
	if err != nil {
		return "", fmt.Errorf("register device: %w", err)
	}

	return deviceID, nil
}

func (r *NotificationRepository) UnregisterDevice(
	ctx context.Context,
	deviceToken string,
) (bool, error) {
	tag, err := r.pool.Exec(
		ctx,
		`DELETE FROM devices WHERE device_token = $1`,
		deviceToken,
	)
	if err != nil {
		return false, fmt.Errorf("unregister device: %w", err)
	}

	return tag.RowsAffected() > 0, nil
}

func (r *NotificationRepository) ListDevices(
	ctx context.Context,
	recipientType notification.RecipientType,
	recipientID string,
) ([]notification.Device, error) {
	rows, err := r.pool.Query(
		ctx,
		`SELECT id, recipient_type, recipient_id, device_token, platform, locale
		 FROM devices
		 WHERE recipient_type = $1 AND recipient_id = $2
		 ORDER BY last_seen_at DESC`,
		string(recipientType),
		recipientID,
	)
	if err != nil {
		return nil, fmt.Errorf("select devices: %w", err)
	}
	defer rows.Close()

	var devices []notification.Device

	for rows.Next() {
		var device notification.Device
		var deviceRecipientType, platform string

		if err := rows.Scan(
			&device.ID,
			&deviceRecipientType,
			&device.RecipientID,
			&device.DeviceToken,
			&platform,
			&device.Locale,
		); err != nil {
			return nil, fmt.Errorf("scan device: %w", err)
		}

		device.RecipientType = notification.RecipientType(deviceRecipientType)
		device.Platform = notification.Platform(platform)

		devices = append(devices, device)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate devices: %w", err)
	}

	return devices, nil
}

func (r *NotificationRepository) RemoveDeviceTokens(
	ctx context.Context,
	tokens []string,
) error {
	if len(tokens) == 0 {
		return nil
	}

	if _, err := r.pool.Exec(
		ctx,
		`DELETE FROM devices WHERE device_token = ANY($1)`,
		tokens,
	); err != nil {
		return fmt.Errorf("remove device tokens: %w", err)
	}

	return nil
}

func (r *NotificationRepository) FindNotificationByIdempotencyKey(
	ctx context.Context,
	key string,
) (notification.Notification, []notification.Delivery, bool, error) {
	row := r.pool.QueryRow(
		ctx,
		notificationSelectSQL+` WHERE idempotency_key = $1`,
		key,
	)

	found, err := scanNotification(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return notification.Notification{}, nil, false, nil
		}

		return notification.Notification{}, nil, false, fmt.Errorf("select notification by key: %w", err)
	}

	deliveries, err := r.listDeliveries(ctx, found.ID)
	if err != nil {
		return notification.Notification{}, nil, false, err
	}

	return found, deliveries, true, nil
}

func (r *NotificationRepository) listDeliveries(
	ctx context.Context,
	notificationID string,
) ([]notification.Delivery, error) {
	rows, err := r.pool.Query(
		ctx,
		`SELECT channel, status, COALESCE(detail, '')
		 FROM notification_deliveries
		 WHERE notification_id = $1`,
		notificationID,
	)
	if err != nil {
		return nil, fmt.Errorf("select deliveries: %w", err)
	}
	defer rows.Close()

	var deliveries []notification.Delivery

	for rows.Next() {
		var delivery notification.Delivery
		var channel, deliveryStatus string

		if err := rows.Scan(&channel, &deliveryStatus, &delivery.Detail); err != nil {
			return nil, fmt.Errorf("scan delivery: %w", err)
		}

		delivery.Channel = notification.Channel(channel)
		delivery.Status = notification.DeliveryStatus(deliveryStatus)

		deliveries = append(deliveries, delivery)
	}

	return deliveries, rows.Err()
}

func (r *NotificationRepository) Persist(
	ctx context.Context,
	input notification.PersistInput,
) (notification.Notification, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return notification.Notification{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	data := input.Data
	if data == nil {
		data = map[string]string{}
	}

	dataJSON, err := json.Marshal(data)
	if err != nil {
		return notification.Notification{}, fmt.Errorf("marshal notification data: %w", err)
	}

	var idempotencyKey *string

	if input.IdempotencyKey != "" {
		idempotencyKey = &input.IdempotencyKey
	}

	row := tx.QueryRow(
		ctx,
		`INSERT INTO notifications
		    (recipient_type, recipient_id, event_key, title, body, locale,
		     data, idempotency_key)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING id, recipient_type, recipient_id, event_key, title, body,
		           locale, data, (read_at IS NOT NULL), created_at`,
		string(input.RecipientType),
		input.RecipientID,
		input.EventKey,
		input.Title,
		input.Body,
		input.Locale,
		dataJSON,
		idempotencyKey,
	)

	persisted, err := scanNotification(row)
	if err != nil {
		return notification.Notification{}, fmt.Errorf("insert notification: %w", err)
	}

	for _, delivery := range input.Deliveries {
		var detail *string

		if delivery.Detail != "" {
			detail = &delivery.Detail
		}

		if _, err := tx.Exec(
			ctx,
			`INSERT INTO notification_deliveries
			    (notification_id, channel, status, detail)
			 VALUES ($1, $2, $3, $4)`,
			persisted.ID,
			string(delivery.Channel),
			string(delivery.Status),
			detail,
		); err != nil {
			return notification.Notification{}, fmt.Errorf("insert delivery: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return notification.Notification{}, fmt.Errorf("commit transaction: %w", err)
	}

	return persisted, nil
}

func (r *NotificationRepository) List(
	ctx context.Context,
	input notification.ListInput,
) ([]notification.Notification, error) {
	query := notificationSelectSQL + ` WHERE recipient_type = $1 AND recipient_id = $2`

	if input.UnreadOnly {
		query += ` AND read_at IS NULL`
	}

	query += ` ORDER BY created_at DESC LIMIT $3`

	rows, err := r.pool.Query(
		ctx,
		query,
		string(input.RecipientType),
		input.RecipientID,
		input.Limit,
	)
	if err != nil {
		return nil, fmt.Errorf("select notifications: %w", err)
	}
	defer rows.Close()

	var notifications []notification.Notification

	for rows.Next() {
		found, err := scanNotification(rows)
		if err != nil {
			return nil, fmt.Errorf("scan notification: %w", err)
		}

		notifications = append(notifications, found)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate notifications: %w", err)
	}

	return notifications, nil
}

func (r *NotificationRepository) UnreadCount(
	ctx context.Context,
	recipientType notification.RecipientType,
	recipientID string,
) (int, error) {
	var count int

	if err := r.pool.QueryRow(
		ctx,
		`SELECT COUNT(*)
		 FROM notifications
		 WHERE recipient_type = $1 AND recipient_id = $2 AND read_at IS NULL`,
		string(recipientType),
		recipientID,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("count unread: %w", err)
	}

	return count, nil
}

// MarkAsRead scopes every update by recipient, even when explicit IDs
// are given — so a caller can't mark someone else's notifications read
// by guessing an ID.
func (r *NotificationRepository) MarkAsRead(
	ctx context.Context,
	recipientType notification.RecipientType,
	recipientID string,
	ids []string,
) (int, error) {
	query := `UPDATE notifications
	          SET read_at = CURRENT_TIMESTAMP
	          WHERE recipient_type = $1 AND recipient_id = $2 AND read_at IS NULL`

	args := []any{string(recipientType), recipientID}

	if len(ids) > 0 {
		query += ` AND id = ANY($3)`
		args = append(args, ids)
	}

	tag, err := r.pool.Exec(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("mark as read: %w", err)
	}

	return int(tag.RowsAffected()), nil
}

const notificationSelectSQL = `SELECT id, recipient_type, recipient_id, event_key,
                                      title, body, locale, data,
                                      (read_at IS NOT NULL), created_at
                               FROM notifications`

type scannable interface {
	Scan(dest ...any) error
}

func scanNotification(row scannable) (notification.Notification, error) {
	var found notification.Notification
	var recipientType string
	var dataJSON []byte

	err := row.Scan(
		&found.ID,
		&recipientType,
		&found.RecipientID,
		&found.EventKey,
		&found.Title,
		&found.Body,
		&found.Locale,
		&dataJSON,
		&found.Read,
		&found.CreatedAt,
	)
	if err != nil {
		return notification.Notification{}, err
	}

	found.RecipientType = notification.RecipientType(recipientType)

	if len(dataJSON) > 0 {
		if err := json.Unmarshal(dataJSON, &found.Data); err != nil {
			return notification.Notification{}, fmt.Errorf("unmarshal notification data: %w", err)
		}
	}

	return found, nil
}

var _ notification.Repository = (*NotificationRepository)(nil)

-- +goose Up

-- The in-app inbox AND the record of what was sent. Rendered title/body
-- are stored rather than re-rendered on read: a notification should
-- always show what the user was actually told, even after the template
-- is edited or the variables it referenced are gone.
CREATE TABLE notifications (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    recipient_type VARCHAR(10) NOT NULL,
    recipient_id UUID NOT NULL,

    event_key VARCHAR(120) NOT NULL,

    title TEXT NOT NULL,
    body TEXT NOT NULL,
    locale VARCHAR(10) NOT NULL,

    data JSONB NOT NULL DEFAULT '{}'::jsonb,

    idempotency_key VARCHAR(160) NULL,

    read_at TIMESTAMPTZ NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT notifications_recipient_type_check
        CHECK (recipient_type IN ('rider', 'driver')),

    CONSTRAINT notifications_data_object_check
        CHECK (jsonb_typeof(data) = 'object')
);

CREATE UNIQUE INDEX notifications_idempotency_key_unique
    ON notifications (idempotency_key)
    WHERE idempotency_key IS NOT NULL;

CREATE INDEX notifications_recipient_created_idx
    ON notifications (recipient_type, recipient_id, created_at DESC);

CREATE INDEX notifications_unread_idx
    ON notifications (recipient_type, recipient_id)
    WHERE read_at IS NULL;

-- Per-channel delivery outcome. Separate from notifications because one
-- notification fans out to several channels, each of which can succeed
-- or fail independently — and a push failing shouldn't hide the message
-- from the in-app inbox.
CREATE TABLE notification_deliveries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    notification_id UUID NOT NULL
        REFERENCES notifications (id) ON DELETE CASCADE,

    channel VARCHAR(20) NOT NULL,
    status VARCHAR(20) NOT NULL,
    detail TEXT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT notification_deliveries_channel_check
        CHECK (channel IN ('in_app', 'push', 'sms')),

    CONSTRAINT notification_deliveries_status_check
        CHECK (status IN ('pending', 'sent', 'failed', 'skipped'))
);

CREATE INDEX notification_deliveries_notification_idx
    ON notification_deliveries (notification_id);

-- +goose Down

DROP TABLE IF EXISTS notification_deliveries;
DROP TABLE IF EXISTS notifications;

-- +goose Up

-- Push tokens, one row per installed app instance. A token is unique
-- globally, not per user: when someone signs out and a colleague signs
-- in on the same handset, the token must move to the new account rather
-- than notify both.
CREATE TABLE devices (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    recipient_type VARCHAR(10) NOT NULL,
    recipient_id UUID NOT NULL,

    device_token TEXT NOT NULL,
    platform VARCHAR(10) NOT NULL,

    -- Per-device, because a household may share a phone in one
    -- language while the account owner prefers another.
    locale VARCHAR(10) NOT NULL DEFAULT 'en',

    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT devices_device_token_unique
        UNIQUE (device_token),

    CONSTRAINT devices_recipient_type_check
        CHECK (recipient_type IN ('rider', 'driver')),

    CONSTRAINT devices_platform_check
        CHECK (platform IN ('android', 'ios', 'web'))
);

CREATE INDEX devices_recipient_idx
    ON devices (
        recipient_type,
        recipient_id
    );

-- +goose Down

DROP TABLE IF EXISTS devices;

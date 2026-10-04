-- +goose Up

-- "Download your data": what a person asked for, and the ZIP once made (a
-- file in media-service, kept until expires_at). The eraser of a deleted
-- account removes these rows with the rest.
CREATE TABLE data_exports (
    id UUID PRIMARY KEY,

    identity_id UUID NOT NULL
        REFERENCES identities (id)
        ON DELETE CASCADE,

    status VARCHAR(10) NOT NULL DEFAULT 'pending',

    requested_at TIMESTAMPTZ NOT NULL,
    ready_at TIMESTAMPTZ NULL,
    expires_at TIMESTAMPTZ NULL,

    media_id UUID NULL,
    size_bytes BIGINT NOT NULL DEFAULT 0,

    -- The maker claims a pending export until next_attempt_at, and tries
    -- again from there after a failure.
    next_attempt_at TIMESTAMPTZ NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error VARCHAR(300) NOT NULL DEFAULT '',

    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT data_exports_status_check
        CHECK (status IN ('pending', 'ready', 'failed', 'expired')),

    CONSTRAINT data_exports_ready_check
        CHECK (status <> 'ready' OR (media_id IS NOT NULL AND ready_at IS NOT NULL AND expires_at IS NOT NULL))
);

CREATE INDEX data_exports_identity_idx
    ON data_exports (identity_id, requested_at DESC);

CREATE INDEX data_exports_pending_idx
    ON data_exports (next_attempt_at)
    WHERE status = 'pending';

CREATE INDEX data_exports_ready_idx
    ON data_exports (expires_at)
    WHERE status = 'ready';

-- +goose Down

DROP TABLE IF EXISTS data_exports;

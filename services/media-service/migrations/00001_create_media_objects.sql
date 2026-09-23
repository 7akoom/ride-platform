-- +goose Up

CREATE TABLE media_objects (
    id UUID PRIMARY KEY,

    owner_identity_id UUID NOT NULL,

    purpose VARCHAR(40) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'pending',

    -- What the client said it would send; the upload URL is signed for it.
    declared_content_type VARCHAR(100) NOT NULL,
    declared_size BIGINT NOT NULL,

    -- What is stored, once ready (images are re-encoded).
    content_type VARCHAR(100) NOT NULL DEFAULT '',
    size_bytes BIGINT NOT NULL DEFAULT 0,
    sha256 CHAR(64) NULL,
    width INTEGER NOT NULL DEFAULT 0,
    height INTEGER NOT NULL DEFAULT 0,

    -- Where the accepted bytes are kept. The client uploads to
    -- 'incoming/' || object_key; only checked bytes are written here, so a
    -- second PUT with a still-valid upload URL cannot replace them.
    object_key VARCHAR(300) NOT NULL,

    -- The incoming object was deleted after its upload URL expired (nothing
    -- can be written there any more).
    upload_cleared BOOLEAN NOT NULL DEFAULT FALSE,
    rejection_reason VARCHAR(200) NOT NULL DEFAULT '',

    -- A service uses the file: the owner cannot delete it.
    held BOOLEAN NOT NULL DEFAULT FALSE,

    created_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT media_objects_object_key_unique
        UNIQUE (object_key),

    CONSTRAINT media_objects_purpose_check
        CHECK (purpose IN ('driver_document', 'profile_photo', 'address_photo', 'support_attachment')),

    CONSTRAINT media_objects_status_check
        CHECK (status IN ('pending', 'ready', 'rejected', 'deleted', 'expired')),

    CONSTRAINT media_objects_declared_size_check
        CHECK (declared_size > 0),

    CONSTRAINT media_objects_ready_check
        CHECK (status <> 'ready' OR (sha256 IS NOT NULL AND size_bytes > 0 AND completed_at IS NOT NULL)),

    CONSTRAINT media_objects_held_check
        CHECK (NOT held OR status = 'ready')
);

CREATE INDEX media_objects_owner_status_idx
    ON media_objects (owner_identity_id, status);

CREATE INDEX media_objects_pending_created_idx
    ON media_objects (created_at)
    WHERE status = 'pending';

CREATE INDEX media_objects_upload_not_cleared_idx
    ON media_objects (created_at)
    WHERE NOT upload_cleared;

-- +goose Down

DROP TABLE IF EXISTS media_objects;

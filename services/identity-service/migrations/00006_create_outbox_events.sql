-- +goose Up

CREATE TABLE outbox_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    aggregate_type VARCHAR(64) NOT NULL,
    aggregate_id UUID NOT NULL,

    event_type VARCHAR(128) NOT NULL,
    schema_version SMALLINT NOT NULL,

    payload JSONB NOT NULL,

    occurred_at TIMESTAMPTZ NOT NULL,
    available_at TIMESTAMPTZ NOT NULL,

    published_at TIMESTAMPTZ NULL,

    publish_attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT outbox_events_aggregate_type_not_blank_check
        CHECK (
            length(btrim(aggregate_type)) > 0
        ),

    CONSTRAINT outbox_events_aggregate_type_trimmed_check
        CHECK (
            aggregate_type = btrim(aggregate_type)
        ),

    CONSTRAINT outbox_events_event_type_not_blank_check
        CHECK (
            length(btrim(event_type)) > 0
        ),

    CONSTRAINT outbox_events_event_type_trimmed_check
        CHECK (
            event_type = btrim(event_type)
        ),

    CONSTRAINT outbox_events_schema_version_positive_check
        CHECK (
            schema_version > 0
        ),

    CONSTRAINT outbox_events_payload_object_check
        CHECK (
            jsonb_typeof(payload) = 'object'
        ),

    CONSTRAINT outbox_events_publish_attempts_non_negative_check
        CHECK (
            publish_attempts >= 0
        )
);

CREATE INDEX outbox_events_pending_idx
    ON outbox_events (
        available_at ASC,
        occurred_at ASC,
        id ASC
    )
    WHERE published_at IS NULL;

CREATE INDEX outbox_events_aggregate_idx
    ON outbox_events (
        aggregate_type,
        aggregate_id,
        occurred_at ASC
    );

-- +goose Down

DROP TABLE outbox_events;
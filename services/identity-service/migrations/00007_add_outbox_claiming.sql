-- +goose Up

ALTER TABLE outbox_events
    ADD COLUMN claim_token UUID NULL;

ALTER TABLE outbox_events
    ADD COLUMN claimed_at TIMESTAMPTZ NULL;

ALTER TABLE outbox_events
    ADD CONSTRAINT outbox_events_claim_state_check
    CHECK (
        (
            claim_token IS NULL
            AND claimed_at IS NULL
        )
        OR
        (
            claim_token IS NOT NULL
            AND claimed_at IS NOT NULL
        )
    );

ALTER TABLE outbox_events
    ADD CONSTRAINT outbox_events_claim_lease_check
    CHECK (
        claim_token IS NULL
        OR available_at > claimed_at
    );

ALTER TABLE outbox_events
    ADD CONSTRAINT outbox_events_published_not_claimed_check
    CHECK (
        published_at IS NULL
        OR claim_token IS NULL
    );

-- +goose Down

ALTER TABLE outbox_events
    DROP CONSTRAINT outbox_events_published_not_claimed_check;

ALTER TABLE outbox_events
    DROP CONSTRAINT outbox_events_claim_lease_check;

ALTER TABLE outbox_events
    DROP CONSTRAINT outbox_events_claim_state_check;

ALTER TABLE outbox_events
    DROP COLUMN claimed_at;

ALTER TABLE outbox_events
    DROP COLUMN claim_token;
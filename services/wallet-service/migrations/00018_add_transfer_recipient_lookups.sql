-- +goose Up

-- Recipient lookups before a transfer, counted per rider so the phones of
-- riders cannot be listed by trying numbers. Only who looked and when; rows
-- older than a day are removed as new ones come in.
CREATE TABLE transfer_recipient_lookups (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    rider_id UUID NOT NULL,
    looked_up_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX transfer_recipient_lookups_rider_idx
    ON transfer_recipient_lookups (rider_id, looked_up_at);

-- +goose Down

DROP TABLE IF EXISTS transfer_recipient_lookups;

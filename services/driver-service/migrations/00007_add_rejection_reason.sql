-- +goose Up

-- Why the last review turned a driver down, shown to the driver. Cleared when
-- the driver is approved.
ALTER TABLE drivers
    ADD COLUMN rejection_reason VARCHAR(500) NOT NULL DEFAULT '';

-- The admin driver list and the review queue (status = 'pending'), newest first.
CREATE INDEX drivers_created_idx
    ON drivers (created_at DESC, id DESC);

CREATE INDEX drivers_status_created_idx
    ON drivers (status, created_at DESC, id DESC);

-- +goose Down

DROP INDEX IF EXISTS drivers_status_created_idx;
DROP INDEX IF EXISTS drivers_created_idx;

ALTER TABLE drivers
    DROP COLUMN IF EXISTS rejection_reason;

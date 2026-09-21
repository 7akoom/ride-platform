-- +goose Up

-- A driver now starts PENDING and only an operator can move it to ACTIVE
-- (or REJECTED). Existing rows keep their current status, so drivers that
-- already work are not affected: only the default for new rows changes.
ALTER TABLE drivers
    DROP CONSTRAINT IF EXISTS drivers_status_check;

ALTER TABLE drivers
    ADD CONSTRAINT drivers_status_check
        CHECK (
            status IN (
                'pending',
                'active',
                'rejected',
                'suspended'
            )
        );

ALTER TABLE drivers
    ALTER COLUMN status SET DEFAULT 'pending';

-- +goose Down

-- The old constraint has no pending/rejected, so those drivers become
-- suspended: they still cannot work, and nobody is approved by a rollback.
UPDATE drivers
SET status = 'suspended'
WHERE status IN ('pending', 'rejected');

ALTER TABLE drivers
    ALTER COLUMN status SET DEFAULT 'active';

ALTER TABLE drivers
    DROP CONSTRAINT IF EXISTS drivers_status_check;

ALTER TABLE drivers
    ADD CONSTRAINT drivers_status_check
        CHECK (
            status IN (
                'active',
                'suspended'
            )
        );

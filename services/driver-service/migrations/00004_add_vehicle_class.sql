-- +goose Up

-- Existing drivers default to economy, so nothing breaks for rows created
-- before vehicle classes existed. Adding a new class later means widening
-- this CHECK in a new migration.
ALTER TABLE drivers
    ADD COLUMN vehicle_class VARCHAR(20) NOT NULL DEFAULT 'economy',
    ADD CONSTRAINT drivers_vehicle_class_check
        CHECK (vehicle_class IN ('economy', 'comfort'));

-- +goose Down

ALTER TABLE drivers
    DROP CONSTRAINT IF EXISTS drivers_vehicle_class_check,
    DROP COLUMN IF EXISTS vehicle_class;

-- +goose Up

-- The class the rider asked for. Trips that predate vehicle classes, and
-- requests that name none, are economy.
ALTER TABLE trips
    ADD COLUMN vehicle_class VARCHAR(20) NOT NULL DEFAULT 'economy',
    ADD CONSTRAINT trips_vehicle_class_check
        CHECK (vehicle_class IN ('economy', 'comfort'));

-- +goose Down

ALTER TABLE trips
    DROP CONSTRAINT IF EXISTS trips_vehicle_class_check,
    DROP COLUMN IF EXISTS vehicle_class;

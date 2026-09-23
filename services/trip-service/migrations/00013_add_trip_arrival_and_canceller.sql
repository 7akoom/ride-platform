-- +goose Up

-- When the driver said they were at the pickup (waiting time and a rider
-- no-show count from here), who cancelled a cancelled trip, and whether the
-- driver cancelled because the rider did not come. Trips cancelled before
-- this migration keep cancelled_by NULL: it was not recorded.
ALTER TABLE trips
    ADD COLUMN arrived_at TIMESTAMPTZ,
    ADD COLUMN cancelled_by VARCHAR(10),
    ADD COLUMN rider_no_show BOOLEAN NOT NULL DEFAULT FALSE,
    ADD CONSTRAINT trips_cancelled_by_check
        CHECK (cancelled_by IS NULL OR cancelled_by IN ('rider', 'driver', 'system')),
    ADD CONSTRAINT trips_rider_no_show_by_driver_check
        CHECK (NOT rider_no_show OR cancelled_by = 'driver');

-- +goose Down

ALTER TABLE trips
    DROP CONSTRAINT IF EXISTS trips_rider_no_show_by_driver_check,
    DROP CONSTRAINT IF EXISTS trips_cancelled_by_check,
    DROP COLUMN rider_no_show,
    DROP COLUMN cancelled_by,
    DROP COLUMN arrived_at;

-- +goose Up
-- The trip list is "this rider's (or this driver's) trips, newest first, page by
-- page": these indexes serve exactly that ordering, so a page costs the same
-- however many trips the rider has taken.
CREATE INDEX IF NOT EXISTS trips_rider_history_idx
    ON trips (rider_id, requested_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS trips_driver_history_idx
    ON trips (driver_id, requested_at DESC, id DESC)
    WHERE driver_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS trips_driver_history_idx;
DROP INDEX IF EXISTS trips_rider_history_idx;

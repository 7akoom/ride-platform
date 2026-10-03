-- +goose Up

-- Where a trip's pickup is served: driver incentives count trips by zone and
-- city. Trips from before this are left without (they match no zone filter).
ALTER TABLE trips
    ADD COLUMN pickup_zone_id UUID,
    ADD COLUMN pickup_city_id UUID;

-- A driver's activity in a period: completed trips, cancellations, offers.
CREATE INDEX trips_driver_completed_idx
    ON trips (driver_id, completed_at)
    WHERE status = 'completed';

CREATE INDEX trips_completed_at_idx
    ON trips (completed_at)
    WHERE status = 'completed';

CREATE INDEX trips_driver_cancelled_idx
    ON trips (driver_id, cancelled_at)
    WHERE cancelled_by = 'driver';

CREATE INDEX trip_offers_driver_offered_idx
    ON trip_offers (driver_id, offered_at);

-- +goose Down

DROP INDEX IF EXISTS trip_offers_driver_offered_idx;
DROP INDEX IF EXISTS trips_driver_cancelled_idx;
DROP INDEX IF EXISTS trips_completed_at_idx;
DROP INDEX IF EXISTS trips_driver_completed_idx;

ALTER TABLE trips
    DROP COLUMN IF EXISTS pickup_city_id,
    DROP COLUMN IF EXISTS pickup_zone_id;

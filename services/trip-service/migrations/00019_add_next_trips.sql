-- +goose Up

-- A driver whose rider is in the car may take a second trip whose pickup is near
-- where they drop that rider, to go to once they do (a "next trip").
--
-- after_trip_id: the trip the driver was on when they took this one.
-- heading_at: when the driver set off for the pickup. For a trip taken as usual
-- that is when it was accepted; for a next trip it stays empty until the trip
-- before it ends. So a driver's current trip is their active one with heading_at
-- set, and their next trip the accepted one without.
-- expected_pickup_at: when the driver was expected at the pickup as they
-- accepted (the road time dispatch offered the trip with).
ALTER TABLE trips
    ADD COLUMN after_trip_id UUID NULL REFERENCES trips (id) ON DELETE SET NULL,
    ADD COLUMN heading_at TIMESTAMPTZ NULL,
    ADD COLUMN expected_pickup_at TIMESTAMPTZ NULL;

UPDATE trips SET heading_at = accepted_at WHERE accepted_at IS NOT NULL;

-- Until now a driver had one active trip at most; the indexes below hold that for
-- the current trip and allow one next trip besides. Stop with a clear message if
-- the data says otherwise, instead of failing on the index.
-- +goose StatementBegin
DO $$
DECLARE
    drivers INTEGER;
BEGIN
    SELECT count(*) INTO drivers FROM (
        SELECT driver_id FROM trips
        WHERE status IN ('accepted', 'in_progress') AND driver_id IS NOT NULL
        GROUP BY driver_id HAVING count(*) > 1
    ) AS doubled;

    IF drivers > 0 THEN
        RAISE EXCEPTION '% driver(s) have more than one accepted or in-progress trip; cancel the extra trips first', drivers;
    END IF;
END
$$;
-- +goose StatementEnd

CREATE UNIQUE INDEX trips_one_current_per_driver
    ON trips (driver_id)
    WHERE status IN ('accepted', 'in_progress') AND heading_at IS NOT NULL;

CREATE UNIQUE INDEX trips_one_next_per_driver
    ON trips (driver_id)
    WHERE status = 'accepted' AND heading_at IS NULL;

CREATE INDEX trips_after_trip_id_idx
    ON trips (after_trip_id)
    WHERE after_trip_id IS NOT NULL;

-- An offer made to a driver on a trip, to take after it, and the road time to the
-- pickup dispatch offered it with.
ALTER TABLE trip_offers
    ADD COLUMN after_trip_id UUID NULL,
    ADD COLUMN pickup_eta_seconds INTEGER NULL
        CONSTRAINT trip_offers_pickup_eta_check CHECK (pickup_eta_seconds IS NULL OR pickup_eta_seconds >= 0);

-- +goose Down

ALTER TABLE trip_offers
    DROP COLUMN IF EXISTS pickup_eta_seconds,
    DROP COLUMN IF EXISTS after_trip_id;

DROP INDEX IF EXISTS trips_after_trip_id_idx;
DROP INDEX IF EXISTS trips_one_next_per_driver;
DROP INDEX IF EXISTS trips_one_current_per_driver;

ALTER TABLE trips
    DROP COLUMN IF EXISTS expected_pickup_at,
    DROP COLUMN IF EXISTS heading_at,
    DROP COLUMN IF EXISTS after_trip_id;

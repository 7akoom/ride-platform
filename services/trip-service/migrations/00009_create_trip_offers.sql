-- +goose Up
-- A trip is offered to one driver at a time. The driver has a short window to
-- accept or reject; expiry is read from expires_at when it matters, so nothing
-- has to run in the background. A driver is offered a given trip at most once,
-- which is how dispatch knows who to skip without keeping any state of its own.
CREATE TABLE trip_offers (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    trip_id      uuid        NOT NULL REFERENCES trips (id) ON DELETE CASCADE,
    driver_id    uuid        NOT NULL,
    status       varchar(20) NOT NULL DEFAULT 'pending',
    offered_at   timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at   timestamptz NOT NULL,
    responded_at timestamptz,
    CONSTRAINT trip_offers_status_check
        CHECK (status IN ('pending', 'accepted', 'rejected', 'expired')),
    CONSTRAINT trip_offers_expiry_check CHECK (expires_at > offered_at),
    CONSTRAINT trip_offers_trip_driver_key UNIQUE (trip_id, driver_id)
);

-- At most one live offer per trip and per driver, enforced by the database so
-- two concurrent offers cannot both succeed.
CREATE UNIQUE INDEX trip_offers_one_pending_per_trip
    ON trip_offers (trip_id) WHERE status = 'pending';

CREATE UNIQUE INDEX trip_offers_one_pending_per_driver
    ON trip_offers (driver_id) WHERE status = 'pending';

-- +goose Down
DROP TABLE IF EXISTS trip_offers;

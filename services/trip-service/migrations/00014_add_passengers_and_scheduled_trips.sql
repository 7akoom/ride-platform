-- +goose Up

-- A trip booked for someone else: who the driver picks up. The rider who
-- booked it pays and follows it.
ALTER TABLE trips
    ADD COLUMN passenger_name VARCHAR(80) NOT NULL DEFAULT '',
    ADD COLUMN passenger_phone VARCHAR(16) NOT NULL DEFAULT '',
    ADD COLUMN scheduled BOOLEAN NOT NULL DEFAULT false,
    ADD CONSTRAINT trips_passenger_check
        CHECK ((passenger_name = '') = (passenger_phone = ''));

-- A trip booked ahead. The scheduler turns it into a trip with the same id
-- shortly before scheduled_at (next_attempt_at), trying again until a
-- deadline after it; a claimed row's next_attempt_at is pushed forward, so
-- two schedulers never dispatch it at once.
CREATE TABLE scheduled_trips (
    id UUID PRIMARY KEY,
    rider_id UUID NOT NULL,
    idempotency_key VARCHAR(120) NOT NULL,

    status VARCHAR(12) NOT NULL DEFAULT 'scheduled',
    scheduled_at TIMESTAMPTZ NOT NULL,
    -- The pickup city's IANA time zone, for showing the time.
    time_zone VARCHAR(64) NOT NULL DEFAULT 'UTC',

    pickup_latitude DOUBLE PRECISION NOT NULL,
    pickup_longitude DOUBLE PRECISION NOT NULL,
    dropoff_latitude DOUBLE PRECISION NOT NULL,
    dropoff_longitude DOUBLE PRECISION NOT NULL,
    pickup_address VARCHAR(300) NOT NULL DEFAULT '',
    dropoff_address VARCHAR(300) NOT NULL DEFAULT '',
    pickup_saved_address_id UUID NULL,
    dropoff_saved_address_id UUID NULL,
    vehicle_class VARCHAR(20) NOT NULL,
    payment_method VARCHAR(10) NOT NULL,
    passenger_name VARCHAR(80) NOT NULL DEFAULT '',
    passenger_phone VARCHAR(16) NOT NULL DEFAULT '',

    next_attempt_at TIMESTAMPTZ NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error VARCHAR(300) NOT NULL DEFAULT '',

    trip_id UUID NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    dispatched_at TIMESTAMPTZ NULL,
    cancelled_at TIMESTAMPTZ NULL,
    failed_at TIMESTAMPTZ NULL,

    CONSTRAINT scheduled_trips_rider_key_unique UNIQUE (rider_id, idempotency_key),
    CONSTRAINT scheduled_trips_status_check
        CHECK (status IN ('scheduled', 'dispatched', 'cancelled', 'failed')),
    CONSTRAINT scheduled_trips_dispatched_check
        CHECK (status <> 'dispatched' OR (trip_id IS NOT NULL AND dispatched_at IS NOT NULL)),
    CONSTRAINT scheduled_trips_passenger_check
        CHECK ((passenger_name = '') = (passenger_phone = ''))
);

-- What the scheduler looks at.
CREATE INDEX scheduled_trips_due_idx
    ON scheduled_trips (next_attempt_at)
    WHERE status = 'scheduled';

CREATE INDEX scheduled_trips_rider_idx
    ON scheduled_trips (rider_id, scheduled_at DESC);

-- +goose Down

DROP TABLE IF EXISTS scheduled_trips;

ALTER TABLE trips
    DROP CONSTRAINT IF EXISTS trips_passenger_check,
    DROP COLUMN IF EXISTS scheduled,
    DROP COLUMN IF EXISTS passenger_phone,
    DROP COLUMN IF EXISTS passenger_name;

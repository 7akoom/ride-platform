-- +goose Up

CREATE TABLE trips (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    rider_id UUID NOT NULL,
    driver_id UUID NULL,

    status VARCHAR(20) NOT NULL DEFAULT 'requested',

    pickup_latitude DOUBLE PRECISION NOT NULL,
    pickup_longitude DOUBLE PRECISION NOT NULL,
    dropoff_latitude DOUBLE PRECISION NOT NULL,
    dropoff_longitude DOUBLE PRECISION NOT NULL,

    cancellation_reason TEXT NULL,

    requested_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    accepted_at TIMESTAMPTZ NULL,
    started_at TIMESTAMPTZ NULL,
    completed_at TIMESTAMPTZ NULL,
    cancelled_at TIMESTAMPTZ NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT trips_status_check
        CHECK (
            status IN (
                'requested',
                'accepted',
                'in_progress',
                'completed',
                'cancelled'
            )
        ),

    -- A driver must be assigned once the trip is accepted or further
    -- along, and must NOT be assigned while still "requested". A
    -- cancelled trip can have gone either way (cancelled before or
    -- after a driver accepted), so it's excluded from this check.
    CONSTRAINT trips_driver_assignment_check
        CHECK (
            (status = 'requested' AND driver_id IS NULL)
            OR (status IN ('accepted', 'in_progress', 'completed') AND driver_id IS NOT NULL)
            OR (status = 'cancelled')
        ),

    CONSTRAINT trips_pickup_latitude_range_check
        CHECK (pickup_latitude BETWEEN -90 AND 90),
    CONSTRAINT trips_pickup_longitude_range_check
        CHECK (pickup_longitude BETWEEN -180 AND 180),
    CONSTRAINT trips_dropoff_latitude_range_check
        CHECK (dropoff_latitude BETWEEN -90 AND 90),
    CONSTRAINT trips_dropoff_longitude_range_check
        CHECK (dropoff_longitude BETWEEN -180 AND 180)
);

CREATE INDEX trips_rider_id_idx
    ON trips (
        rider_id
    );

CREATE INDEX trips_driver_id_idx
    ON trips (
        driver_id
    )
    WHERE driver_id IS NOT NULL;

CREATE INDEX trips_status_idx
    ON trips (
        status
    );

-- +goose Down

DROP TABLE IF EXISTS trips;

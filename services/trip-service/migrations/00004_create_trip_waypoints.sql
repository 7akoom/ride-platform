-- +goose Up

CREATE TABLE trip_waypoints (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    trip_id UUID NOT NULL REFERENCES trips (id) ON DELETE CASCADE,

    latitude DOUBLE PRECISION NOT NULL,
    longitude DOUBLE PRECISION NOT NULL,

    recorded_at TIMESTAMPTZ NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT trip_waypoints_latitude_range_check
        CHECK (latitude BETWEEN -90 AND 90),
    CONSTRAINT trip_waypoints_longitude_range_check
        CHECK (longitude BETWEEN -180 AND 180)
);

-- Every read is "give me this trip's path in order" — this single
-- index serves both that read and the throttling check (latest
-- recorded_at for a trip) in RecordWaypointIfDue.
CREATE INDEX trip_waypoints_trip_id_recorded_at_idx
    ON trip_waypoints (
        trip_id,
        recorded_at ASC
    );

-- +goose Down

DROP TABLE IF EXISTS trip_waypoints;

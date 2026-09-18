-- +goose Up

CREATE TABLE sos_alerts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    trip_id UUID NOT NULL REFERENCES trips (id) ON DELETE CASCADE,

    triggered_by VARCHAR(10) NOT NULL,

    latitude DOUBLE PRECISION NOT NULL,
    longitude DOUBLE PRECISION NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT sos_alerts_triggered_by_check
        CHECK (triggered_by IN ('rider', 'driver')),

    CONSTRAINT sos_alerts_latitude_range_check
        CHECK (latitude BETWEEN -90 AND 90),
    CONSTRAINT sos_alerts_longitude_range_check
        CHECK (longitude BETWEEN -180 AND 180)
);

-- Every alert is looked up by which trip it belongs to (the safety
-- team's entry point is always "trip X just had an SOS"), never in
-- bulk across trips.
CREATE INDEX sos_alerts_trip_id_idx
    ON sos_alerts (
        trip_id
    );

-- +goose Down

DROP TABLE IF EXISTS sos_alerts;

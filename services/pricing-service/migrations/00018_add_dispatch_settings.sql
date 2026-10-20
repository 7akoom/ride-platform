-- +goose Up

-- How trips are given out, set by staff (dispatch.manage) and read by
-- dispatch-service: whether a driver whose rider is in the car may be offered a
-- trip whose pickup is near where they drop that rider, and how near.
CREATE TABLE dispatch_settings (
    id BOOLEAN PRIMARY KEY DEFAULT true,

    next_trips_enabled BOOLEAN NOT NULL DEFAULT true,
    next_trip_radius_meters INTEGER NOT NULL DEFAULT 3000,

    updated_by UUID NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT dispatch_settings_one_row_check CHECK (id),
    CONSTRAINT dispatch_settings_radius_check
        CHECK (next_trip_radius_meters BETWEEN 500 AND 10000)
);

INSERT INTO dispatch_settings (id) VALUES (true);

-- +goose Down

DROP TABLE IF EXISTS dispatch_settings;

-- +goose Up

-- Up to 2 stops on the way from pickup to dropoff, in order:
-- [{"latitude": .., "longitude": .., "address": "..", "reached_at": null}].
-- reached_at is set when the driver marks the stop.
ALTER TABLE trips
    ADD COLUMN stops JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD CONSTRAINT trips_stops_check
        CHECK (jsonb_typeof(stops) = 'array' AND jsonb_array_length(stops) <= 2);

-- A booking keeps its stops too, and hands them to its trip.
ALTER TABLE scheduled_trips
    ADD COLUMN stops JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD CONSTRAINT scheduled_trips_stops_check
        CHECK (jsonb_typeof(stops) = 'array' AND jsonb_array_length(stops) <= 2);

-- +goose Down

ALTER TABLE scheduled_trips
    DROP CONSTRAINT scheduled_trips_stops_check,
    DROP COLUMN stops;

ALTER TABLE trips
    DROP CONSTRAINT trips_stops_check,
    DROP COLUMN stops;

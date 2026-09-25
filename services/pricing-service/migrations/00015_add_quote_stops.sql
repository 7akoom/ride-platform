-- +goose Up

-- A quote priced through stops on the way keeps them, in order
-- ([{"latitude": .., "longitude": ..}]): a trip requested with it must have
-- the same ones.
ALTER TABLE fare_quotes
    ADD COLUMN stops JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD CONSTRAINT fare_quotes_stops_check
        CHECK (jsonb_typeof(stops) = 'array' AND jsonb_array_length(stops) <= 2);

-- +goose Down

ALTER TABLE fare_quotes
    DROP CONSTRAINT fare_quotes_stops_check,
    DROP COLUMN stops;

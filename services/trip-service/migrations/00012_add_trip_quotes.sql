-- +goose Up

-- A trip requested with a fare quote (pricing-service QuoteTrip) keeps the
-- quote and the price it fixed, so the rider and the captain see it on the
-- trip. All three stay NULL for a trip priced when it completes.
ALTER TABLE trips
    ADD COLUMN quote_id UUID,
    ADD COLUMN quoted_fare NUMERIC(12, 4),
    ADD COLUMN currency_code VARCHAR(3),
    ADD CONSTRAINT trips_quote_fields_together_check
        CHECK ((quote_id IS NULL) = (quoted_fare IS NULL) AND (quote_id IS NULL) = (currency_code IS NULL)),
    ADD CONSTRAINT trips_quoted_fare_non_negative_check
        CHECK (quoted_fare IS NULL OR quoted_fare >= 0);

-- A quote pays for one trip only.
CREATE UNIQUE INDEX trips_quote_id_unique
    ON trips (quote_id)
    WHERE quote_id IS NOT NULL;

-- +goose Down

DROP INDEX IF EXISTS trips_quote_id_unique;

ALTER TABLE trips
    DROP CONSTRAINT IF EXISTS trips_quoted_fare_non_negative_check,
    DROP CONSTRAINT IF EXISTS trips_quote_fields_together_check,
    DROP COLUMN currency_code,
    DROP COLUMN quoted_fare,
    DROP COLUMN quote_id;

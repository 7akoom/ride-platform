-- +goose Up

-- Rate cards for a city, not only a zone or everywhere, and every price the
-- trip lifecycle charges on one card: the minimum fare, waiting at the
-- pickup, cancelling and not showing up, and how much surge may add.
-- Existing cards keep working unchanged: no minimum, no fees (staff turn
-- them on by setting a card), surge capped at 150% with demand and weather
-- counting, as before.
ALTER TABLE pricing_configs
    ADD COLUMN city_id UUID,
    ADD COLUMN minimum_fare NUMERIC(12, 4) NOT NULL DEFAULT 0,
    ADD COLUMN free_waiting_minutes INTEGER NOT NULL DEFAULT 3,
    ADD COLUMN waiting_per_minute NUMERIC(12, 4) NOT NULL DEFAULT 0,
    ADD COLUMN cancellation_fee NUMERIC(12, 4) NOT NULL DEFAULT 0,
    ADD COLUMN cancellation_grace_minutes INTEGER NOT NULL DEFAULT 2,
    ADD COLUMN no_show_fee NUMERIC(12, 4) NOT NULL DEFAULT 0,
    ADD COLUMN max_surge_percent NUMERIC(6, 2) NOT NULL DEFAULT 150,
    ADD COLUMN demand_surge BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN weather_surge BOOLEAN NOT NULL DEFAULT TRUE,
    -- A retired version takes its place's card away: trips there fall back
    -- to the next card. The card for every class everywhere never retires.
    ADD COLUMN retired BOOLEAN NOT NULL DEFAULT FALSE,
    -- The staff member (identity id) who set this version; NULL for seeds.
    ADD COLUMN created_by UUID,
    ADD CONSTRAINT pricing_configs_one_place_check
        CHECK (zone_id IS NULL OR city_id IS NULL),
    ADD CONSTRAINT pricing_configs_minimum_fare_non_negative_check
        CHECK (minimum_fare >= 0),
    ADD CONSTRAINT pricing_configs_free_waiting_minutes_range_check
        CHECK (free_waiting_minutes BETWEEN 0 AND 60),
    ADD CONSTRAINT pricing_configs_waiting_per_minute_non_negative_check
        CHECK (waiting_per_minute >= 0),
    ADD CONSTRAINT pricing_configs_cancellation_fee_non_negative_check
        CHECK (cancellation_fee >= 0),
    ADD CONSTRAINT pricing_configs_cancellation_grace_minutes_range_check
        CHECK (cancellation_grace_minutes BETWEEN 0 AND 60),
    ADD CONSTRAINT pricing_configs_no_show_fee_non_negative_check
        CHECK (no_show_fee >= 0),
    ADD CONSTRAINT pricing_configs_max_surge_percent_range_check
        CHECK (max_surge_percent BETWEEN 0 AND 300),
    ADD CONSTRAINT pricing_configs_default_never_retired_check
        CHECK (NOT (retired AND zone_id IS NULL AND city_id IS NULL AND vehicle_class IS NULL));

CREATE INDEX pricing_configs_city_class_created_at_idx
    ON pricing_configs (city_id, vehicle_class, created_at DESC);

-- Surge rules for a city or a zone, not only everywhere. Their hours are
-- read in the local time of the pickup's city.
ALTER TABLE surge_time_rules
    ADD COLUMN zone_id UUID,
    ADD COLUMN city_id UUID,
    ADD CONSTRAINT surge_time_rules_one_place_check
        CHECK (zone_id IS NULL OR city_id IS NULL),
    ADD CONSTRAINT surge_time_rules_surge_percent_range_check
        CHECK (surge_percent <= 300);

-- A surge staff put on one zone for a while (a concert, a storm).
CREATE TABLE zone_surges (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    zone_id UUID NOT NULL,
    surge_percent NUMERIC(6, 2) NOT NULL,
    reason VARCHAR(80) NOT NULL,
    starts_at TIMESTAMPTZ NOT NULL,
    ends_at TIMESTAMPTZ NOT NULL,
    -- Set when staff end it early (or call off one that had not started).
    ended_at TIMESTAMPTZ,
    created_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT zone_surges_percent_range_check
        CHECK (surge_percent > 0 AND surge_percent <= 300),
    CONSTRAINT zone_surges_window_check
        CHECK (ends_at > starts_at)
);

CREATE INDEX zone_surges_running_idx
    ON zone_surges (zone_id, ends_at)
    WHERE ended_at IS NULL;

-- A price for one vehicle class, held for a few minutes. A trip requested
-- with it claims it (once) and pays exactly its total. Unclaimed quotes
-- are also the demand signal: riders asking for prices in a zone.
CREATE TABLE fare_quotes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    rider_id UUID NOT NULL,
    zone_id UUID NOT NULL,
    city_id UUID,
    vehicle_class VARCHAR(20) NOT NULL,

    pickup_latitude DOUBLE PRECISION NOT NULL,
    pickup_longitude DOUBLE PRECISION NOT NULL,
    dropoff_latitude DOUBLE PRECISION NOT NULL,
    dropoff_longitude DOUBLE PRECISION NOT NULL,

    currency_code VARCHAR(3) NOT NULL,
    total NUMERIC(12, 4) NOT NULL,
    -- The whole fare breakdown the rider was shown.
    breakdown JSONB NOT NULL,
    -- The rate card version it was priced with.
    config_id UUID REFERENCES pricing_configs (id),
    coupon_id UUID REFERENCES coupons (id),
    discount_amount NUMERIC(12, 4) NOT NULL DEFAULT 0,

    drivers_available BOOLEAN NOT NULL DEFAULT FALSE,
    pickup_eta_minutes INTEGER NOT NULL DEFAULT 0,

    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,

    claimed_trip_id UUID,
    claimed_at TIMESTAMPTZ,

    CONSTRAINT fare_quotes_claimed_trip_unique UNIQUE (claimed_trip_id),
    CONSTRAINT fare_quotes_total_non_negative_check CHECK (total >= 0),
    CONSTRAINT fare_quotes_expiry_check CHECK (expires_at > created_at)
);

CREATE INDEX fare_quotes_zone_created_at_idx
    ON fare_quotes (zone_id, created_at);

CREATE INDEX fare_quotes_unclaimed_expiry_idx
    ON fare_quotes (expires_at)
    WHERE claimed_trip_id IS NULL;

-- What a fare was priced with, for support and audit.
ALTER TABLE fares
    ADD COLUMN city_id UUID,
    ADD COLUMN surge_zone_percent NUMERIC(6, 2) NOT NULL DEFAULT 0,
    ADD COLUMN surge_label VARCHAR(80),
    ADD COLUMN minimum_fare_adjustment NUMERIC(12, 4) NOT NULL DEFAULT 0,
    ADD COLUMN quote_id UUID,
    ADD COLUMN config_id UUID;

-- +goose Down

ALTER TABLE fares
    DROP COLUMN config_id,
    DROP COLUMN quote_id,
    DROP COLUMN minimum_fare_adjustment,
    DROP COLUMN surge_label,
    DROP COLUMN surge_zone_percent,
    DROP COLUMN city_id;

DROP TABLE IF EXISTS fare_quotes;
DROP TABLE IF EXISTS zone_surges;

-- Rules for one city or zone would apply everywhere without their place.
DELETE FROM surge_time_rules WHERE zone_id IS NOT NULL OR city_id IS NOT NULL;

ALTER TABLE surge_time_rules
    DROP CONSTRAINT IF EXISTS surge_time_rules_surge_percent_range_check,
    DROP CONSTRAINT IF EXISTS surge_time_rules_one_place_check,
    DROP COLUMN city_id,
    DROP COLUMN zone_id;

-- City cards and retired versions mean nothing without their columns.
DELETE FROM pricing_configs WHERE city_id IS NOT NULL OR retired;

DROP INDEX IF EXISTS pricing_configs_city_class_created_at_idx;

ALTER TABLE pricing_configs
    DROP CONSTRAINT IF EXISTS pricing_configs_default_never_retired_check,
    DROP CONSTRAINT IF EXISTS pricing_configs_max_surge_percent_range_check,
    DROP CONSTRAINT IF EXISTS pricing_configs_no_show_fee_non_negative_check,
    DROP CONSTRAINT IF EXISTS pricing_configs_cancellation_grace_minutes_range_check,
    DROP CONSTRAINT IF EXISTS pricing_configs_cancellation_fee_non_negative_check,
    DROP CONSTRAINT IF EXISTS pricing_configs_waiting_per_minute_non_negative_check,
    DROP CONSTRAINT IF EXISTS pricing_configs_free_waiting_minutes_range_check,
    DROP CONSTRAINT IF EXISTS pricing_configs_minimum_fare_non_negative_check,
    DROP CONSTRAINT IF EXISTS pricing_configs_one_place_check,
    DROP COLUMN created_by,
    DROP COLUMN retired,
    DROP COLUMN weather_surge,
    DROP COLUMN demand_surge,
    DROP COLUMN max_surge_percent,
    DROP COLUMN no_show_fee,
    DROP COLUMN cancellation_grace_minutes,
    DROP COLUMN cancellation_fee,
    DROP COLUMN waiting_per_minute,
    DROP COLUMN free_waiting_minutes,
    DROP COLUMN minimum_fare,
    DROP COLUMN city_id;

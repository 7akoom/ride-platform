-- +goose Up

-- Versioned on purpose: a new row is inserted whenever rates change, and
-- the latest one (by created_at) is "active". This gives a free audit
-- trail of rate history instead of silently overwriting numbers that
-- past fares were calculated from.
CREATE TABLE pricing_configs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    currency_code VARCHAR(3) NOT NULL,

    base_fare NUMERIC(12, 4) NOT NULL,
    per_km_rate NUMERIC(12, 4) NOT NULL,
    per_minute_rate NUMERIC(12, 4) NOT NULL,

    -- Used to estimate trip duration from straight-line distance, since
    -- there's no routing engine yet (see location-service's README for
    -- the same straight-line-distance trade-off).
    average_speed_kmh NUMERIC(6, 2) NOT NULL DEFAULT 30,

    -- Straight-line distance undercounts real road distance (roads
    -- curve, pickup/dropoff aren't connected by a straight line). This
    -- factor corrects for that until a real routing engine is added.
    distance_correction_factor NUMERIC(4, 2) NOT NULL DEFAULT 1.3,

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT pricing_configs_currency_code_format_check
        CHECK (currency_code ~ '^[A-Z]{3}$'),
    CONSTRAINT pricing_configs_base_fare_non_negative_check
        CHECK (base_fare >= 0),
    CONSTRAINT pricing_configs_per_km_rate_non_negative_check
        CHECK (per_km_rate >= 0),
    CONSTRAINT pricing_configs_per_minute_rate_non_negative_check
        CHECK (per_minute_rate >= 0),
    CONSTRAINT pricing_configs_average_speed_positive_check
        CHECK (average_speed_kmh > 0),
    CONSTRAINT pricing_configs_distance_correction_factor_positive_check
        CHECK (distance_correction_factor >= 1)
);

CREATE INDEX pricing_configs_created_at_idx
    ON pricing_configs (
        created_at DESC
    );

-- +goose Down

DROP TABLE IF EXISTS pricing_configs;

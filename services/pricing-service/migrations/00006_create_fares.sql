-- +goose Up

-- One row per trip, written exactly once by CalculateFare. If
-- CalculateFare is called again for the same trip_id (e.g. a retried
-- request), the existing row is returned instead of recalculating —
-- a trip's price must not change after the fact just because the
-- surge/coupon inputs happened to be different on a second call.
CREATE TABLE fares (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    trip_id UUID NOT NULL,
    rider_id UUID NOT NULL,

    currency_code VARCHAR(3) NOT NULL,

    base_fare NUMERIC(12, 4) NOT NULL,
    distance_km NUMERIC(10, 3) NOT NULL,
    distance_fare NUMERIC(12, 4) NOT NULL,
    duration_minutes NUMERIC(10, 2) NOT NULL,
    duration_fare NUMERIC(12, 4) NOT NULL,
    subtotal NUMERIC(12, 4) NOT NULL,

    surge_time_percent NUMERIC(6, 2) NOT NULL DEFAULT 0,
    surge_demand_percent NUMERIC(6, 2) NOT NULL DEFAULT 0,
    surge_weather_percent NUMERIC(6, 2) NOT NULL DEFAULT 0,
    surge_total_percent NUMERIC(6, 2) NOT NULL DEFAULT 0,
    surge_amount NUMERIC(12, 4) NOT NULL DEFAULT 0,

    applied_discount_type VARCHAR(20) NULL,
    applied_discount_label VARCHAR(80) NULL,
    discount_amount NUMERIC(12, 4) NOT NULL DEFAULT 0,

    total NUMERIC(12, 4) NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT fares_trip_id_unique
        UNIQUE (trip_id),

    CONSTRAINT fares_total_non_negative_check
        CHECK (total >= 0)
);

CREATE INDEX fares_rider_id_idx
    ON fares (
        rider_id
    );

-- +goose Down

DROP TABLE IF EXISTS fares;

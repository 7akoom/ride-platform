-- +goose Up

-- Deliberately self-contained: rather than Pricing calling Trip service
-- to ask "how many rides has this rider completed", it keeps its own
-- counter, incremented once per CalculateFare call (which only happens
-- once per completed trip). This avoids adding a new cross-service
-- dependency just for a discount-eligibility check.
CREATE TABLE rider_trip_stats (
    rider_id UUID PRIMARY KEY,

    completed_trip_count INTEGER NOT NULL DEFAULT 0,

    first_completed_at TIMESTAMPTZ NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT rider_trip_stats_completed_trip_count_non_negative_check
        CHECK (completed_trip_count >= 0)
);

-- +goose Down

DROP TABLE IF EXISTS rider_trip_stats;

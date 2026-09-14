-- +goose Up

CREATE TABLE surge_time_rules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    label VARCHAR(80) NOT NULL,

    -- NULL day_of_week means "every day". 0 = Sunday .. 6 = Saturday,
    -- matching PostgreSQL's EXTRACT(DOW FROM ...) so lookups are a
    -- direct comparison, no day-name translation needed.
    day_of_week SMALLINT NULL,

    start_time TIME NOT NULL,
    end_time TIME NOT NULL,

    -- Stored as an "extra" percentage (30 means +30%), not a multiplier,
    -- so multiple active surge sources can be summed directly. See
    -- pricing.go's surge calculation for how time/demand/weather extras
    -- combine.
    surge_percent NUMERIC(6, 2) NOT NULL,

    active BOOLEAN NOT NULL DEFAULT true,

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT surge_time_rules_day_of_week_range_check
        CHECK (day_of_week IS NULL OR day_of_week BETWEEN 0 AND 6),
    CONSTRAINT surge_time_rules_surge_percent_non_negative_check
        CHECK (surge_percent >= 0)
);

CREATE INDEX surge_time_rules_active_idx
    ON surge_time_rules (
        active
    )
    WHERE active = true;

-- +goose Down

DROP TABLE IF EXISTS surge_time_rules;

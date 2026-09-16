-- +goose Up

CREATE EXTENSION IF NOT EXISTS postgis;

CREATE TABLE zones (
    id UUID PRIMARY KEY,

    city VARCHAR(100) NOT NULL,
    name VARCHAR(120) NOT NULL,

    -- Geography, not geometry: correct point-in-polygon semantics on
    -- the Earth's curved surface without needing a projected SRID per
    -- region. SRID 4326 = WGS84, the lat/lng system every coordinate
    -- in this codebase already uses.
    boundary GEOGRAPHY(POLYGON, 4326) NOT NULL,

    active BOOLEAN NOT NULL DEFAULT TRUE,

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT zones_city_not_blank_check
        CHECK (length(btrim(city)) > 0),

    CONSTRAINT zones_name_not_blank_check
        CHECK (length(btrim(name)) > 0)
);

-- Spatial index: makes "which zone (if any) contains this point" a fast
-- indexed lookup instead of a sequential scan over every polygon.
CREATE INDEX zones_boundary_gist_idx ON zones USING GIST (boundary);

CREATE INDEX zones_city_idx ON zones (city);

-- +goose Down

DROP TABLE IF EXISTS zones;

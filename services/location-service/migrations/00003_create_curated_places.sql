-- +goose Up

CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- Curated places: the airports, malls, hotels... staff chose, named in every
-- language, each with the exact point to be picked up or dropped at.
CREATE TABLE curated_places (
    id UUID PRIMARY KEY,

    city_id UUID NOT NULL REFERENCES cities (id) ON DELETE RESTRICT,

    category VARCHAR(20) NOT NULL,

    name VARCHAR(120) NOT NULL,
    -- The name in other languages: {"ar": "...", "ku": "...", "en": "..."}.
    names JSONB NOT NULL DEFAULT '{}'::jsonb,

    address VARCHAR(300) NOT NULL DEFAULT '',

    location GEOGRAPHY(POINT, 4326) NOT NULL,

    -- Higher first in lists and search.
    priority INTEGER NOT NULL DEFAULT 0,

    active BOOLEAN NOT NULL DEFAULT TRUE,

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    -- Every name, lower case, for search.
    search_text TEXT GENERATED ALWAYS AS (
        lower(
            name || ' ' ||
            coalesce(names ->> 'ar', '') || ' ' ||
            coalesce(names ->> 'ku', '') || ' ' ||
            coalesce(names ->> 'en', '')
        )
    ) STORED,

    CONSTRAINT curated_places_category_check
        CHECK (category IN ('airport', 'mall', 'hotel', 'hospital', 'university',
                            'landmark', 'station', 'government', 'restaurant', 'other')),

    CONSTRAINT curated_places_name_not_blank_check
        CHECK (length(btrim(name)) > 0 AND name = btrim(name)),

    CONSTRAINT curated_places_names_object_check
        CHECK (jsonb_typeof(names) = 'object'),

    CONSTRAINT curated_places_priority_check
        CHECK (priority BETWEEN -1000 AND 1000)
);

CREATE INDEX curated_places_city_idx ON curated_places (city_id, active, priority DESC);
CREATE INDEX curated_places_location_gist_idx ON curated_places USING GIST (location);
CREATE INDEX curated_places_search_trgm_idx ON curated_places USING GIN (search_text gin_trgm_ops);

-- +goose Down

DROP TABLE IF EXISTS curated_places;

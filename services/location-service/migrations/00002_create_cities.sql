-- +goose Up

-- A deployment serves one country in one currency; a city adds its own time
-- zone, the point a map of it opens at, and the zones inside it.
CREATE TABLE cities (
    id UUID PRIMARY KEY,

    name VARCHAR(120) NOT NULL,
    -- The name in other languages: {"ar": "...", "ku": "...", "en": "..."}.
    names JSONB NOT NULL DEFAULT '{}'::jsonb,

    -- IANA time zone ("Asia/Baghdad"), checked by the service.
    time_zone VARCHAR(64) NOT NULL,

    center_latitude DOUBLE PRECISION NOT NULL,
    center_longitude DOUBLE PRECISION NOT NULL,

    active BOOLEAN NOT NULL DEFAULT TRUE,

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT cities_name_not_blank_check
        CHECK (length(btrim(name)) > 0 AND name = btrim(name)),

    CONSTRAINT cities_names_object_check
        CHECK (jsonb_typeof(names) = 'object'),

    CONSTRAINT cities_time_zone_not_blank_check
        CHECK (length(btrim(time_zone)) > 0),

    CONSTRAINT cities_center_check
        CHECK (center_latitude BETWEEN -90 AND 90 AND center_longitude BETWEEN -180 AND 180)
);

CREATE UNIQUE INDEX cities_name_lower_unique ON cities (lower(name));

-- Every city named by an existing zone becomes a city, centred on its zones.
-- The time zone is not known here: UTC until staff set it (PATCH
-- /v1/admin/cities/{id}).
INSERT INTO cities (id, name, time_zone, center_latitude, center_longitude)
SELECT
    gen_random_uuid(),
    min(btrim(city)),
    'UTC',
    ST_Y(ST_Centroid(ST_Collect(boundary::geometry))),
    ST_X(ST_Centroid(ST_Collect(boundary::geometry)))
FROM zones
GROUP BY lower(btrim(city));

ALTER TABLE zones ADD COLUMN city_id UUID NULL;

UPDATE zones z
SET city_id = c.id
FROM cities c
WHERE lower(c.name) = lower(btrim(z.city));

ALTER TABLE zones
    ALTER COLUMN city_id SET NOT NULL,
    ADD CONSTRAINT zones_city_fk FOREIGN KEY (city_id) REFERENCES cities (id) ON DELETE RESTRICT;

DROP INDEX IF EXISTS zones_city_idx;
ALTER TABLE zones DROP CONSTRAINT IF EXISTS zones_city_not_blank_check;
ALTER TABLE zones DROP COLUMN city;

CREATE INDEX zones_city_id_idx ON zones (city_id);

-- +goose Down

ALTER TABLE zones ADD COLUMN city VARCHAR(100) NULL;

UPDATE zones z
SET city = left(c.name, 100)
FROM cities c
WHERE c.id = z.city_id;

ALTER TABLE zones
    ALTER COLUMN city SET NOT NULL,
    ADD CONSTRAINT zones_city_not_blank_check CHECK (length(btrim(city)) > 0);

CREATE INDEX zones_city_idx ON zones (city);

DROP INDEX IF EXISTS zones_city_id_idx;
ALTER TABLE zones DROP CONSTRAINT IF EXISTS zones_city_fk;
ALTER TABLE zones DROP COLUMN city_id;

DROP TABLE IF EXISTS cities;

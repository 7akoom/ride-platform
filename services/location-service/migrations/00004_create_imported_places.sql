-- +goose Up

-- One way to compare names, for every place search: lower case, the letters
-- Arabic, Kurdish and Persian write differently made the same (أ إ آ -> ا,
-- ى ی ئ ێ -> ي, ة ە ھ -> ه, ک -> ك, ڵ -> ل ...), Eastern digits made Western,
-- marks and tatweel dropped, accents taken off Latin letters, separators made
-- spaces. Searches run it on what the rider typed and on every stored name.
-- +goose StatementBegin
CREATE FUNCTION place_search_text(value TEXT) RETURNS TEXT
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
    SELECT btrim(regexp_replace(
        translate(lower(coalesce(value, '')),
                  'أإآٱىیئێةەھۀکؤۆڵڕ٠١٢٣٤٥٦٧٨٩۰۱۲۳۴۵۶۷۸۹áàâäãåāéèêëēíìîïīóòôöõōúùûüūçñ-_.,،/\ًٌٍَُِّْٰـ''’',
                  'ااااييييههههكوولر01234567890123456789aaaaaaaeeeeeiiiiioooooouuuuucn       '),
        '\s+', ' ', 'g'))
$$;
-- +goose StatementEnd

-- Curated places compare their names the same way.
ALTER TABLE curated_places DROP COLUMN search_text;
ALTER TABLE curated_places ADD COLUMN search_text TEXT GENERATED ALWAYS AS (
    place_search_text(
        name || ' ' ||
        coalesce(names ->> 'ar', '') || ' ' ||
        coalesce(names ->> 'ku', '') || ' ' ||
        coalesce(names ->> 'en', '')
    )
) STORED;
CREATE INDEX curated_places_search_trgm_idx ON curated_places USING GIN (search_text gin_trgm_ops);

-- Places imported from an open places dataset (Overture Maps) for the
-- instance's own service zones: scripts/tools/import-places fills it and
-- replaces it whole on every run. Search shows them after curated places and
-- before the map's own results. Staff never edit them; a place that matters
-- is made a curated place instead.
CREATE TABLE imported_places (
    -- The dataset's own id, stable between its releases.
    id VARCHAR(100) PRIMARY KEY,

    name VARCHAR(300) NOT NULL,
    -- Other names (other languages, the brand), space separated, for search.
    alt_names TEXT NOT NULL DEFAULT '',

    -- What the apps show it as: the curated places' categories.
    kind VARCHAR(20) NOT NULL DEFAULT 'other',
    -- The dataset's own category, as it gave it.
    category VARCHAR(100) NOT NULL DEFAULT '',

    address VARCHAR(300) NOT NULL DEFAULT '',
    location GEOGRAPHY(POINT, 4326) NOT NULL,

    -- How strongly its kind ranks (the import's settings), and how sure the
    -- dataset is that it exists (0 to 1).
    weight REAL NOT NULL DEFAULT 1,
    confidence REAL NOT NULL DEFAULT 0,

    source VARCHAR(40) NOT NULL,
    release VARCHAR(40) NOT NULL DEFAULT '',
    imported_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    search_text TEXT GENERATED ALWAYS AS (place_search_text(name || ' ' || alt_names)) STORED,

    CONSTRAINT imported_places_kind_check
        CHECK (kind IN ('airport', 'mall', 'hotel', 'hospital', 'university',
                        'landmark', 'station', 'government', 'restaurant', 'other')),

    CONSTRAINT imported_places_name_not_blank_check
        CHECK (length(btrim(name)) > 0)
);

CREATE INDEX imported_places_search_trgm_idx ON imported_places USING GIN (search_text gin_trgm_ops);
CREATE INDEX imported_places_location_gist_idx ON imported_places USING GIST (location);

-- +goose Down

DROP TABLE IF EXISTS imported_places;

ALTER TABLE curated_places DROP COLUMN search_text;
ALTER TABLE curated_places ADD COLUMN search_text TEXT GENERATED ALWAYS AS (
    lower(
        name || ' ' ||
        coalesce(names ->> 'ar', '') || ' ' ||
        coalesce(names ->> 'ku', '') || ' ' ||
        coalesce(names ->> 'en', '')
    )
) STORED;
CREATE INDEX curated_places_search_trgm_idx ON curated_places USING GIN (search_text gin_trgm_ops);

DROP FUNCTION IF EXISTS place_search_text(TEXT);

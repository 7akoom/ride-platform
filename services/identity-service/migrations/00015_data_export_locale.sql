-- +goose Up

-- The language the person asked for their data in: the export's index.html (its
-- tables, for reading in a browser) is written in it.
ALTER TABLE data_exports
    ADD COLUMN locale VARCHAR(5) NOT NULL DEFAULT 'en'
        CONSTRAINT data_exports_locale_check CHECK (locale IN ('en', 'ar', 'ku'));

-- +goose Down

ALTER TABLE data_exports DROP COLUMN IF EXISTS locale;

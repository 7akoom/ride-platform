-- +goose Up

-- One row per event type. Channels are stored as a text array so a
-- deployment can decide, per event, whether it warrants a push, an SMS,
-- or only an in-app entry — without a code change.
CREATE TABLE notification_templates (
    event_key VARCHAR(120) PRIMARY KEY,

    default_channels TEXT[] NOT NULL DEFAULT ARRAY['in_app', 'push'],

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT notification_templates_event_key_not_blank_check
        CHECK (length(btrim(event_key)) > 0),

    CONSTRAINT notification_templates_channels_not_empty_check
        CHECK (cardinality(default_channels) > 0)
);

-- Translations live in their own table rather than as columns, so
-- adding a language is inserting rows — not a migration. This matters
-- here: the platform targets Arabic, Kurdish and English markets, and
-- a deployment may need a language the original build never
-- anticipated.
CREATE TABLE notification_template_translations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    event_key VARCHAR(120) NOT NULL
        REFERENCES notification_templates (event_key) ON DELETE CASCADE,

    locale VARCHAR(10) NOT NULL,

    -- Bodies use {variable} placeholders, substituted at send time.
    title TEXT NOT NULL,
    body TEXT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT notification_template_translations_unique
        UNIQUE (event_key, locale),

    CONSTRAINT notification_template_translations_locale_not_blank_check
        CHECK (length(btrim(locale)) > 0)
);

-- +goose Down

DROP TABLE IF EXISTS notification_template_translations;
DROP TABLE IF EXISTS notification_templates;

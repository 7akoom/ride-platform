-- +goose Up

CREATE TABLE identities (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    phone_number VARCHAR(16) NOT NULL,

    status VARCHAR(20) NOT NULL DEFAULT 'active',

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT identities_phone_number_unique
        UNIQUE (phone_number),

    CONSTRAINT identities_phone_number_format_check
        CHECK (phone_number ~ '^\+[1-9][0-9]{1,14}$'),

    CONSTRAINT identities_status_check
        CHECK (status IN ('active', 'suspended', 'disabled'))
);

-- +goose Down

DROP TABLE IF EXISTS identities;
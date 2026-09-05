-- +goose Up

CREATE TABLE identities (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    status VARCHAR(20) NOT NULL DEFAULT 'active',

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT identities_status_check
        CHECK (
            status IN (
                'active',
                'suspended',
                'disabled'
            )
        )
);


CREATE TABLE identity_identifiers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    identity_id UUID NOT NULL,

    identifier_type VARCHAR(20) NOT NULL,
    normalized_value VARCHAR(254) NOT NULL,

    verified_at TIMESTAMPTZ NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT identity_identifiers_identity_id_fk
        FOREIGN KEY (identity_id)
        REFERENCES identities (id)
        ON DELETE CASCADE,

    CONSTRAINT identity_identifiers_type_check
        CHECK (
            identifier_type IN (
                'phone',
                'email'
            )
        ),

    CONSTRAINT identity_identifiers_value_not_blank_check
        CHECK (
            length(btrim(normalized_value)) > 0
        ),

    CONSTRAINT identity_identifiers_value_trimmed_check
        CHECK (
            normalized_value = btrim(normalized_value)
        ),

    CONSTRAINT identity_identifiers_phone_format_check
        CHECK (
            identifier_type <> 'phone'
            OR normalized_value ~ '^\+[1-9][0-9]{1,14}$'
        ),

    CONSTRAINT identity_identifiers_email_length_check
        CHECK (
            identifier_type <> 'email'
            OR length(normalized_value) <= 254
        ),

    CONSTRAINT identity_identifiers_email_canonical_case_check
        CHECK (
            identifier_type <> 'email'
            OR normalized_value = lower(normalized_value)
        ),

    CONSTRAINT identity_identifiers_type_value_unique
        UNIQUE (
            identifier_type,
            normalized_value
        )
);

CREATE INDEX identity_identifiers_identity_id_idx
    ON identity_identifiers (
        identity_id
    );

CREATE INDEX identity_identifiers_type_identity_idx
    ON identity_identifiers (
        identifier_type,
        identity_id
    );


-- +goose Down

DROP TABLE IF EXISTS identity_identifiers;
DROP TABLE IF EXISTS identities;
-- +goose Up

CREATE TABLE riders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    identity_id UUID NOT NULL,

    display_name VARCHAR(120) NOT NULL,

    status VARCHAR(20) NOT NULL DEFAULT 'active',

    rating_average NUMERIC(3, 2) NOT NULL DEFAULT 5.00,
    rating_count INTEGER NOT NULL DEFAULT 0,

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT riders_identity_id_unique
        UNIQUE (identity_id),

    CONSTRAINT riders_status_check
        CHECK (
            status IN (
                'active',
                'suspended'
            )
        ),

    CONSTRAINT riders_display_name_not_blank_check
        CHECK (
            length(btrim(display_name)) > 0
        ),

    CONSTRAINT riders_display_name_trimmed_check
        CHECK (
            display_name = btrim(display_name)
        ),

    CONSTRAINT riders_rating_average_range_check
        CHECK (
            rating_average >= 0
            AND rating_average <= 5
        ),

    CONSTRAINT riders_rating_count_non_negative_check
        CHECK (
            rating_count >= 0
        )
);

CREATE INDEX riders_identity_id_idx
    ON riders (
        identity_id
    );

-- +goose Down

DROP TABLE IF EXISTS riders;

-- +goose Up

-- A rider's optional personal details and picture, apart from the profile
-- row every service reads.
CREATE TABLE rider_details (
    rider_id UUID PRIMARY KEY REFERENCES riders (id) ON DELETE CASCADE,
    gender VARCHAR(10) NOT NULL DEFAULT '',
    date_of_birth DATE NULL,
    nationality VARCHAR(2) NOT NULL DEFAULT '',
    photo_media_id UUID NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT rider_details_gender_check CHECK (gender IN ('', 'male', 'female')),
    CONSTRAINT rider_details_nationality_check CHECK (nationality = '' OR nationality ~ '^[A-Z]{2}$')
);

-- +goose Down

DROP TABLE rider_details;

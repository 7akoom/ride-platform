-- +goose Up

-- A rider's saved addresses: home, work and other places, with what helps
-- the captain find them.
CREATE TABLE saved_addresses (
    id UUID PRIMARY KEY,

    rider_id UUID NOT NULL REFERENCES riders (id) ON DELETE CASCADE,

    kind VARCHAR(10) NOT NULL,
    label VARCHAR(60) NOT NULL DEFAULT '',

    latitude DOUBLE PRECISION NOT NULL,
    longitude DOUBLE PRECISION NOT NULL,

    address VARCHAR(300) NOT NULL DEFAULT '',
    details VARCHAR(200) NOT NULL DEFAULT '',
    note_for_driver VARCHAR(300) NOT NULL DEFAULT '',

    -- media-service file (purpose ADDRESS_PHOTO), held for as long as the
    -- address has it.
    photo_media_id UUID NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT saved_addresses_kind_check
        CHECK (kind IN ('home', 'work', 'other')),

    CONSTRAINT saved_addresses_other_label_check
        CHECK (kind <> 'other' OR length(btrim(label)) > 0),

    CONSTRAINT saved_addresses_coordinates_check
        CHECK (latitude BETWEEN -90 AND 90 AND longitude BETWEEN -180 AND 180)
);

CREATE INDEX saved_addresses_rider_idx ON saved_addresses (rider_id);

-- One home and one work per rider.
CREATE UNIQUE INDEX saved_addresses_one_home_work_idx
    ON saved_addresses (rider_id, kind)
    WHERE kind IN ('home', 'work');

-- +goose Down

DROP TABLE IF EXISTS saved_addresses;

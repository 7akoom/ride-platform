-- +goose Up

-- The addresses as the rider picked them, and what the saved pickup address
-- tells the captain (copied at request time, so later edits of the saved
-- address do not change a trip).
ALTER TABLE trips
    ADD COLUMN pickup_address VARCHAR(300) NOT NULL DEFAULT '',
    ADD COLUMN dropoff_address VARCHAR(300) NOT NULL DEFAULT '',
    ADD COLUMN pickup_details VARCHAR(200) NOT NULL DEFAULT '',
    ADD COLUMN pickup_note VARCHAR(300) NOT NULL DEFAULT '',
    ADD COLUMN pickup_photo_media_id UUID NULL;

-- Recent destinations: a rider's completed trips, newest first.
CREATE INDEX trips_rider_completed_idx
    ON trips (rider_id, completed_at DESC)
    WHERE status = 'completed';

-- +goose Down

DROP INDEX IF EXISTS trips_rider_completed_idx;

ALTER TABLE trips
    DROP COLUMN pickup_photo_media_id,
    DROP COLUMN pickup_note,
    DROP COLUMN pickup_details,
    DROP COLUMN dropoff_address,
    DROP COLUMN pickup_address;

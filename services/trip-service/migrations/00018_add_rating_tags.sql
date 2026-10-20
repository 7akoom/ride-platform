-- +goose Up

-- What the rating was for, beside the stars: the rider picks tags about the
-- captain ("safe driving", "clean car"...), the captain tags about the rider.
-- Kept by the operating company with the comment; the person rated sees none.
ALTER TABLE trip_ratings
    ADD COLUMN tags TEXT[] NOT NULL DEFAULT '{}';

-- +goose Down

ALTER TABLE trip_ratings DROP COLUMN IF EXISTS tags;

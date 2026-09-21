-- +goose Up

-- One row per rating already folded into a profile's average. trip.rated is delivered at
-- least once, so a redelivered event must not count twice: the rating id is the primary key,
-- and the average is only changed by the transaction that manages to insert it.
CREATE TABLE processed_rating_events (
    rating_id UUID PRIMARY KEY,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +goose Down

DROP TABLE IF EXISTS processed_rating_events;

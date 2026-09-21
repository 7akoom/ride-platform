-- +goose Up

-- One rating per side per trip: the rider rates the driver and the driver rates the rider.
-- The comment stays here for the operating company's Admin; it is never published in an
-- event and never shown to the person rated. The rider's and the driver's running
-- averages live in their own services and are updated from the trip.rated event.
CREATE TABLE trip_ratings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    trip_id UUID NOT NULL REFERENCES trips (id) ON DELETE CASCADE,

    -- Who gave the rating, and which profile received it.
    rated_by VARCHAR(10) NOT NULL,
    rater_id UUID NOT NULL,
    ratee_id UUID NOT NULL,

    stars SMALLINT NOT NULL,
    comment VARCHAR(500) NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT trip_ratings_rated_by_check
        CHECK (rated_by IN ('rider', 'driver')),

    CONSTRAINT trip_ratings_stars_check
        CHECK (stars BETWEEN 1 AND 5),

    CONSTRAINT trip_ratings_comment_not_blank_check
        CHECK (comment IS NULL OR length(btrim(comment)) > 0),

    -- Each side rates a trip at most once; this is also what makes a retry harmless.
    CONSTRAINT trip_ratings_trip_rated_by_unique
        UNIQUE (trip_id, rated_by)
);

-- "What has this driver / rider been rated" for the Admin.
CREATE INDEX trip_ratings_ratee_idx
    ON trip_ratings (
        ratee_id,
        created_at DESC
    );

-- +goose Down

DROP TABLE IF EXISTS trip_ratings;

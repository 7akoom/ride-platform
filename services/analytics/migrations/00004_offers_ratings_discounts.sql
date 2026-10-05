-- +goose Up
-- +goose StatementBegin

-- What a trip's fare was given off and what surge added; the stars each
-- side gave (rider_stars: the rider rating the driver).
ALTER TABLE trip_facts
    ADD COLUMN discount_amount NUMERIC(18,3),
    ADD COLUMN surge_amount    NUMERIC(18,3),
    ADD COLUMN rider_stars     SMALLINT,
    ADD COLUMN driver_stars    SMALLINT,
    ADD CONSTRAINT trip_facts_rider_stars_check CHECK (rider_stars BETWEEN 1 AND 5),
    ADD CONSTRAINT trip_facts_driver_stars_check CHECK (driver_stars BETWEEN 1 AND 5);

CREATE INDEX trip_facts_completed_at_idx ON trip_facts (completed_at) WHERE completed_at IS NOT NULL;

-- One row per offer of a trip to a driver (never twice the same pair). No
-- outcome and past expires_at = it ran out.
CREATE TABLE offer_facts (
    trip_id    TEXT NOT NULL,
    driver_id  TEXT NOT NULL,
    offered_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    outcome    TEXT,
    decided_at TIMESTAMPTZ,
    PRIMARY KEY (trip_id, driver_id),
    CONSTRAINT offer_facts_outcome_check CHECK (outcome IS NULL OR outcome IN ('accepted', 'rejected'))
);

CREATE INDEX offer_facts_offered_at_idx ON offer_facts (offered_at);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE offer_facts;
DROP INDEX trip_facts_completed_at_idx;
ALTER TABLE trip_facts
    DROP COLUMN discount_amount,
    DROP COLUMN surge_amount,
    DROP COLUMN rider_stars,
    DROP COLUMN driver_stars;
-- +goose StatementEnd

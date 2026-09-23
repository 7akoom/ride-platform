-- +goose Up

-- A fare is the trip's, or the fee of a cancelled trip (the rider cancelled
-- after the grace minutes, or did not come). And the trip's fare carries
-- what waiting at the pickup beyond the free minutes cost.
ALTER TABLE fares
    ADD COLUMN kind VARCHAR(20) NOT NULL DEFAULT 'trip',
    ADD COLUMN waiting_minutes INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN waiting_fare NUMERIC(12, 4) NOT NULL DEFAULT 0,
    ADD CONSTRAINT fares_kind_check
        CHECK (kind IN ('trip', 'cancellation', 'no_show')),
    ADD CONSTRAINT fares_waiting_non_negative_check
        CHECK (waiting_minutes >= 0 AND waiting_fare >= 0);

-- +goose Down

-- A fee is not a trip's fare: without the column it would read as one.
DELETE FROM fares WHERE kind <> 'trip';

ALTER TABLE fares
    DROP CONSTRAINT IF EXISTS fares_waiting_non_negative_check,
    DROP CONSTRAINT IF EXISTS fares_kind_check,
    DROP COLUMN waiting_fare,
    DROP COLUMN waiting_minutes,
    DROP COLUMN kind;

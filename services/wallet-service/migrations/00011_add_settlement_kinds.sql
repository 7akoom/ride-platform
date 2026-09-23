-- +goose Up

-- A settlement is a trip's fare, or the fee of a cancelled trip (the rider
-- cancelled late or did not come). A fee is taken from the rider's wallet;
-- what the wallet could not cover is due_amount, still owed by the rider.
-- The driver is paid the fee minus the commission either way.
ALTER TABLE trip_settlements
    ADD COLUMN kind VARCHAR(20) NOT NULL DEFAULT 'trip',
    ADD COLUMN due_amount NUMERIC(16, 3) NOT NULL DEFAULT 0,
    ADD CONSTRAINT trip_settlements_kind_check
        CHECK (kind IN ('trip', 'cancellation', 'no_show')),
    ADD CONSTRAINT trip_settlements_due_non_negative_check
        CHECK (due_amount >= 0),
    ADD CONSTRAINT trip_settlements_due_only_for_fees_check
        CHECK (kind <> 'trip' OR due_amount = 0);

CREATE INDEX trip_settlements_rider_due_idx
    ON trip_settlements (rider_id)
    WHERE due_amount > 0;

-- +goose Down

DROP INDEX IF EXISTS trip_settlements_rider_due_idx;

ALTER TABLE trip_settlements
    DROP CONSTRAINT IF EXISTS trip_settlements_due_only_for_fees_check,
    DROP CONSTRAINT IF EXISTS trip_settlements_due_non_negative_check,
    DROP CONSTRAINT IF EXISTS trip_settlements_kind_check,
    DROP COLUMN due_amount,
    DROP COLUMN kind;

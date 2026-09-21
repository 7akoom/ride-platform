-- +goose Up

-- The rider hands the driver more cash than the trip's cash part and the driver has no
-- change: the difference is credited to the rider's wallet instead. The platform pays for
-- it (nothing is taken from the driver), so this table is what a deployment audits: who
-- recorded how much, on which trip.
ALTER TABLE wallet_transactions
    DROP CONSTRAINT IF EXISTS wallet_transactions_type_check;

ALTER TABLE wallet_transactions
    ADD CONSTRAINT wallet_transactions_type_check
        CHECK (
            type IN (
                'top_up',
                'trip_payment',
                'trip_earning',
                'commission',
                'payout',
                'adjustment',
                'change_credit'
            )
        );

-- The most change one trip may credit. It sits on the versioned config like the commission,
-- so a deployment changes it with a new wallet_configs row (newest wins).
ALTER TABLE wallet_configs
    ADD COLUMN max_change_credit NUMERIC(16, 3) NOT NULL DEFAULT 2000,
    ADD CONSTRAINT wallet_configs_max_change_credit_non_negative_check
        CHECK (max_change_credit >= 0);

CREATE TABLE trip_change_credits (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- One credit per trip: the UNIQUE constraint is what makes recording it idempotent.
    trip_id UUID NOT NULL REFERENCES trip_settlements (trip_id),

    rider_id UUID NOT NULL,
    driver_id UUID NOT NULL,

    currency_code VARCHAR(3) NOT NULL,

    -- The cash part of the fare, everything the rider handed over, and the difference.
    cash_due NUMERIC(16, 3) NOT NULL,
    cash_received NUMERIC(16, 3) NOT NULL,
    change_amount NUMERIC(16, 3) NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT trip_change_credits_trip_id_unique
        UNIQUE (trip_id),

    CONSTRAINT trip_change_credits_change_positive_check
        CHECK (change_amount > 0),

    CONSTRAINT trip_change_credits_adds_up_check
        CHECK (cash_received = cash_due + change_amount)
);

-- "How much change did each driver record this month" for the Admin.
CREATE INDEX trip_change_credits_driver_idx
    ON trip_change_credits (
        driver_id,
        created_at DESC
    );

-- +goose Down

DROP TABLE IF EXISTS trip_change_credits;

ALTER TABLE wallet_configs
    DROP CONSTRAINT IF EXISTS wallet_configs_max_change_credit_non_negative_check,
    DROP COLUMN IF EXISTS max_change_credit;

-- The old constraint has no change_credit: those rows keep their amount and become adjustments.
UPDATE wallet_transactions
SET type = 'adjustment'
WHERE type = 'change_credit';

ALTER TABLE wallet_transactions
    DROP CONSTRAINT IF EXISTS wallet_transactions_type_check;

ALTER TABLE wallet_transactions
    ADD CONSTRAINT wallet_transactions_type_check
        CHECK (
            type IN (
                'top_up',
                'trip_payment',
                'trip_earning',
                'commission',
                'payout',
                'adjustment'
            )
        );

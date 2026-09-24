-- +goose Up

-- Top-ups go through any payment provider, for any wallet: the ZainCash
-- table becomes the providers' one. Existing rows are drivers' ZainCash
-- top-ups, which the defaults say.
ALTER TABLE zaincash_topups RENAME TO provider_topups;
ALTER TABLE provider_topups RENAME COLUMN driver_id TO owner_id;
ALTER TABLE provider_topups RENAME COLUMN zaincash_transaction_id TO provider_transaction_id;
ALTER TABLE provider_topups RENAME CONSTRAINT zaincash_topups_amount_positive_check TO provider_topups_amount_positive_check;
ALTER TABLE provider_topups RENAME CONSTRAINT zaincash_topups_status_check TO provider_topups_status_check;
ALTER INDEX zaincash_topups_driver_id_idx RENAME TO provider_topups_owner_idx;

ALTER TABLE provider_topups
    ADD COLUMN owner_type VARCHAR(10) NOT NULL DEFAULT 'driver',
    ADD COLUMN provider VARCHAR(20) NOT NULL DEFAULT 'zaincash',
    ADD CONSTRAINT provider_topups_owner_type_check CHECK (owner_type IN ('rider', 'driver'));

-- The least and most one top-up may bring, and one tip may give.
ALTER TABLE wallet_configs
    ADD COLUMN topup_min_amount NUMERIC(16, 3) NOT NULL DEFAULT 1000,
    ADD COLUMN topup_max_amount NUMERIC(16, 3) NOT NULL DEFAULT 1000000,
    ADD COLUMN tip_min_amount NUMERIC(16, 3) NOT NULL DEFAULT 250,
    ADD COLUMN tip_max_amount NUMERIC(16, 3) NOT NULL DEFAULT 25000,
    ADD CONSTRAINT wallet_configs_topup_limits_check
        CHECK (topup_min_amount > 0 AND topup_max_amount >= topup_min_amount),
    ADD CONSTRAINT wallet_configs_tip_limits_check
        CHECK (tip_min_amount > 0 AND tip_max_amount >= tip_min_amount);

-- A rider's tip for a completed trip: once per trip, all of it to the
-- driver.
CREATE TABLE trip_tips (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    trip_id UUID NOT NULL,
    rider_id UUID NOT NULL,
    driver_id UUID NOT NULL,

    currency_code VARCHAR(3) NOT NULL,
    amount NUMERIC(16, 3) NOT NULL,

    idempotency_key VARCHAR(120) NOT NULL,
    rider_transaction_id UUID NOT NULL REFERENCES wallet_transactions (id),
    driver_transaction_id UUID NOT NULL REFERENCES wallet_transactions (id),

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT trip_tips_trip_unique UNIQUE (trip_id),
    CONSTRAINT trip_tips_rider_key_unique UNIQUE (rider_id, idempotency_key),
    CONSTRAINT trip_tips_amount_positive_check CHECK (amount > 0)
);

CREATE INDEX trip_tips_driver_idx ON trip_tips (driver_id, created_at DESC);

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
                'change_credit',
                'transfer_out',
                'transfer_in',
                'due_payment',
                'voucher',
                'refund',
                'payout_return',
                'tip'
            )
        );

-- +goose Down

-- The old constraint has no tip: those rows keep their amount and become
-- adjustments.
UPDATE wallet_transactions
SET type = 'adjustment'
WHERE type = 'tip';

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
                'change_credit',
                'transfer_out',
                'transfer_in',
                'due_payment',
                'voucher',
                'refund',
                'payout_return'
            )
        );

DROP TABLE IF EXISTS trip_tips;

ALTER TABLE wallet_configs
    DROP CONSTRAINT IF EXISTS wallet_configs_tip_limits_check,
    DROP CONSTRAINT IF EXISTS wallet_configs_topup_limits_check,
    DROP COLUMN IF EXISTS tip_max_amount,
    DROP COLUMN IF EXISTS tip_min_amount,
    DROP COLUMN IF EXISTS topup_max_amount,
    DROP COLUMN IF EXISTS topup_min_amount;

-- The old table is drivers' ZainCash top-ups only: other rows go.
DELETE FROM provider_topups WHERE owner_type <> 'driver' OR provider <> 'zaincash';

ALTER TABLE provider_topups
    DROP CONSTRAINT IF EXISTS provider_topups_owner_type_check,
    DROP COLUMN IF EXISTS provider,
    DROP COLUMN IF EXISTS owner_type;

ALTER INDEX provider_topups_owner_idx RENAME TO zaincash_topups_driver_id_idx;
ALTER TABLE provider_topups RENAME CONSTRAINT provider_topups_status_check TO zaincash_topups_status_check;
ALTER TABLE provider_topups RENAME CONSTRAINT provider_topups_amount_positive_check TO zaincash_topups_amount_positive_check;
ALTER TABLE provider_topups RENAME COLUMN provider_transaction_id TO zaincash_transaction_id;
ALTER TABLE provider_topups RENAME COLUMN owner_id TO driver_id;
ALTER TABLE provider_topups RENAME TO zaincash_topups;

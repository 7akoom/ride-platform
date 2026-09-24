-- +goose Up

-- A rider sends money from their wallet to another registered rider's, by
-- the phone the other signs in with, confirmed with their wallet PIN. The
-- limits sit on the versioned config like the commission (newest row wins):
-- the least and most one transfer may move, and how much and how many a
-- rider may send in any 24 hours.
ALTER TABLE wallet_configs
    ADD COLUMN transfer_min_amount NUMERIC(16, 3) NOT NULL DEFAULT 250,
    ADD COLUMN transfer_max_amount NUMERIC(16, 3) NOT NULL DEFAULT 1000000,
    ADD COLUMN transfer_daily_amount NUMERIC(16, 3) NOT NULL DEFAULT 2000000,
    ADD COLUMN transfer_daily_count INTEGER NOT NULL DEFAULT 20,
    ADD CONSTRAINT wallet_configs_transfer_limits_check
        CHECK (
            transfer_min_amount > 0
            AND transfer_max_amount >= transfer_min_amount
            AND transfer_daily_amount >= transfer_max_amount
            AND transfer_daily_count > 0
        );

CREATE TABLE wallet_transfers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    sender_rider_id UUID NOT NULL,
    recipient_rider_id UUID NOT NULL,

    -- The phones as they were at the time, for each side's history.
    sender_phone VARCHAR(16) NOT NULL DEFAULT '',
    recipient_phone VARCHAR(16) NOT NULL,

    currency_code VARCHAR(3) NOT NULL,
    amount NUMERIC(16, 3) NOT NULL,
    note VARCHAR(140) NOT NULL DEFAULT '',

    -- The sender's key: a retried send with the same key is the same
    -- transfer, never a second one.
    idempotency_key VARCHAR(120) NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT wallet_transfers_sender_key_unique
        UNIQUE (sender_rider_id, idempotency_key),

    CONSTRAINT wallet_transfers_amount_positive_check
        CHECK (amount > 0),

    CONSTRAINT wallet_transfers_not_to_self_check
        CHECK (sender_rider_id <> recipient_rider_id)
);

CREATE INDEX wallet_transfers_sender_idx
    ON wallet_transfers (sender_rider_id, created_at DESC);

CREATE INDEX wallet_transfers_recipient_idx
    ON wallet_transfers (recipient_rider_id, created_at DESC);

-- Each side of a transfer is a ledger row pointing at it.
ALTER TABLE wallet_transactions
    ADD COLUMN transfer_id UUID NULL REFERENCES wallet_transfers (id);

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
                'transfer_in'
            )
        );

-- +goose Down

-- The old constraint has no transfers: those rows keep their amount and
-- become adjustments.
UPDATE wallet_transactions
SET type = 'adjustment'
WHERE type IN ('transfer_out', 'transfer_in');

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

ALTER TABLE wallet_transactions
    DROP COLUMN IF EXISTS transfer_id;

DROP TABLE IF EXISTS wallet_transfers;

ALTER TABLE wallet_configs
    DROP CONSTRAINT IF EXISTS wallet_configs_transfer_limits_check,
    DROP COLUMN IF EXISTS transfer_daily_count,
    DROP COLUMN IF EXISTS transfer_daily_amount,
    DROP COLUMN IF EXISTS transfer_max_amount,
    DROP COLUMN IF EXISTS transfer_min_amount;

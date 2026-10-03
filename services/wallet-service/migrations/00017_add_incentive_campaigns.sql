-- +goose Up

-- Incentive campaigns: complete N trips in a period (in a scope), get paid
-- the bonus of the highest tier reached, if the conditions are met. The
-- platform pays it into the driver's wallet once the campaign has ended.
CREATE TABLE incentive_campaigns (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    name VARCHAR(80) NOT NULL,
    description VARCHAR(500) NOT NULL DEFAULT '',
    currency_code VARCHAR(3) NOT NULL,

    starts_at TIMESTAMPTZ NOT NULL,
    ends_at TIMESTAMPTZ NOT NULL,

    -- Which trips count; NULL or empty means any.
    city_id UUID,
    zone_ids UUID[] NOT NULL DEFAULT '{}',
    vehicle_class VARCHAR(20) NOT NULL DEFAULT '',
    -- Daily hours, minutes after local midnight in time_zone; equal means
    -- all day. May cross midnight.
    daily_start_minute SMALLINT NOT NULL DEFAULT 0,
    daily_end_minute SMALLINT NOT NULL DEFAULT 0,
    time_zone VARCHAR(64) NOT NULL,

    -- Conditions; NULL means none.
    min_acceptance_rate NUMERIC(5, 2),
    max_cancellation_rate NUMERIC(5, 2),
    min_rating NUMERIC(3, 2),

    -- [{"trips": 30, "amount": "25000"}, ...], trips and amounts increasing.
    tiers JSONB NOT NULL,

    -- active (scheduled or running by the clock), settling, settled, cancelled.
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    cancel_reason VARCHAR(500) NOT NULL DEFAULT '',
    settle_lease_until TIMESTAMPTZ,
    settled_at TIMESTAMPTZ,

    created_by UUID,
    idempotency_key VARCHAR(120),

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT incentive_campaigns_period_check CHECK (ends_at > starts_at),
    CONSTRAINT incentive_campaigns_status_check
        CHECK (status IN ('active', 'settling', 'settled', 'cancelled')),
    CONSTRAINT incentive_campaigns_class_check
        CHECK (vehicle_class IN ('', 'economy', 'comfort')),
    CONSTRAINT incentive_campaigns_hours_check
        CHECK (daily_start_minute BETWEEN 0 AND 1440 AND daily_end_minute BETWEEN 0 AND 1440),
    CONSTRAINT incentive_campaigns_rates_check
        CHECK (
            (min_acceptance_rate IS NULL OR min_acceptance_rate BETWEEN 0 AND 100)
            AND (max_cancellation_rate IS NULL OR max_cancellation_rate BETWEEN 0 AND 100)
            AND (min_rating IS NULL OR min_rating BETWEEN 0 AND 5)
        ),
    CONSTRAINT incentive_campaigns_tiers_check CHECK (jsonb_typeof(tiers) = 'array' AND jsonb_array_length(tiers) > 0)
);

CREATE UNIQUE INDEX incentive_campaigns_idempotency_key_unique
    ON incentive_campaigns (idempotency_key)
    WHERE idempotency_key IS NOT NULL;

CREATE INDEX incentive_campaigns_due_idx
    ON incentive_campaigns (ends_at)
    WHERE status IN ('active', 'settling');

CREATE INDEX incentive_campaigns_created_idx
    ON incentive_campaigns (created_at DESC, id DESC);

-- What each driver who reached a tier got, or why not. One row per campaign
-- and driver: settling again after a crash skips the drivers already done.
CREATE TABLE incentive_payouts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    campaign_id UUID NOT NULL REFERENCES incentive_campaigns (id),
    driver_id UUID NOT NULL,

    completed_trips INTEGER NOT NULL,
    acceptance_rate NUMERIC(5, 2) NOT NULL,
    cancellation_rate NUMERIC(5, 2) NOT NULL,
    -- NULL when the driver has no rating yet.
    rating NUMERIC(3, 2),

    tier_trips INTEGER NOT NULL,
    amount NUMERIC(16, 3) NOT NULL,
    status VARCHAR(20) NOT NULL,
    unmet TEXT[] NOT NULL DEFAULT '{}',
    transaction_id UUID REFERENCES wallet_transactions (id),

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT incentive_payouts_campaign_driver_unique UNIQUE (campaign_id, driver_id),
    CONSTRAINT incentive_payouts_status_check CHECK (status IN ('paid', 'not_eligible')),
    CONSTRAINT incentive_payouts_paid_check
        CHECK ((status = 'paid') = (transaction_id IS NOT NULL AND amount > 0))
);

CREATE INDEX incentive_payouts_driver_idx
    ON incentive_payouts (driver_id, created_at DESC);

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
                'tip',
                'incentive'
            )
        );

-- +goose Down

-- The old constraint has no incentive: those rows keep their amount and
-- become adjustments.
UPDATE wallet_transactions
SET type = 'adjustment'
WHERE type = 'incentive';

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

DROP TABLE IF EXISTS incentive_payouts;
DROP TABLE IF EXISTS incentive_campaigns;

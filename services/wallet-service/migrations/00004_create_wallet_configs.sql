-- +goose Up

-- Versioned like pricing_configs: rates change by inserting a new row,
-- newest wins, old rows stay as an audit trail of what past settlements
-- were calculated under.
CREATE TABLE wallet_configs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    currency_code VARCHAR(3) NOT NULL,

    -- Platform's cut, as a percentage of the fare (20 means 20%).
    commission_rate NUMERIC(5, 2) NOT NULL,

    -- Drivers work on a PREPAID commission balance: they deposit money
    -- up front and commission is drawn from it. This is how the platform
    -- collects its cut in a cash-dominant market, where the driver
    -- physically holds the fare and the platform has no other way to be
    -- paid.
    --
    -- suspension_threshold is how far below zero the balance may fall
    -- before the account stops receiving trips ENTIRELY (not just cash
    -- ones) until the driver tops up. Stored positive; suspension
    -- triggers at balance <= -suspension_threshold. Set it to 0 to
    -- require a driver to always be in credit.
    suspension_threshold NUMERIC(16, 3) NOT NULL,

    -- Minimum a driver must have before they can request a payout.
    minimum_payout_amount NUMERIC(16, 3) NOT NULL DEFAULT 0,

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT wallet_configs_currency_code_format_check
        CHECK (currency_code ~ '^[A-Z]{3}$'),

    CONSTRAINT wallet_configs_commission_rate_range_check
        CHECK (commission_rate >= 0 AND commission_rate <= 100),

    CONSTRAINT wallet_configs_suspension_threshold_non_negative_check
        CHECK (suspension_threshold >= 0),

    CONSTRAINT wallet_configs_minimum_payout_non_negative_check
        CHECK (minimum_payout_amount >= 0)
);

CREATE INDEX wallet_configs_created_at_idx
    ON wallet_configs (
        created_at DESC
    );

-- +goose Down

DROP TABLE IF EXISTS wallet_configs;

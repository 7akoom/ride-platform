-- +goose Up

-- Money is NUMERIC, never float. NUMERIC is exact fixed-point in
-- Postgres: 2451.6 stores and sums as exactly 2451.6, with no binary
-- floating-point drift. The Go side mirrors this with shopspring/decimal,
-- so a balance can be displayed as-is without a minor-units conversion
-- while still being safe to add and subtract thousands of times.
--
-- Scale 3 covers currencies down to three decimal places (IQD's fils,
-- BHD, KWD, OMR). Currencies with fewer decimals simply don't use them.
CREATE TABLE wallets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    owner_type VARCHAR(10) NOT NULL,
    owner_id UUID NOT NULL,

    currency_code VARCHAR(3) NOT NULL,

    -- Deliberately allowed to go negative: a driver who takes cash trips
    -- has collected the platform's commission on the platform's behalf,
    -- so their digital balance goes negative until it's worked off or
    -- settled. See settle_trip.go for the full explanation.
    balance NUMERIC(16, 3) NOT NULL DEFAULT 0,

    blocked BOOLEAN NOT NULL DEFAULT false,

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT wallets_owner_unique
        UNIQUE (owner_type, owner_id),

    CONSTRAINT wallets_owner_type_check
        CHECK (owner_type IN ('rider', 'driver')),

    CONSTRAINT wallets_currency_code_format_check
        CHECK (currency_code ~ '^[A-Z]{3}$'),

    -- A rider's wallet is prepaid and must never go negative; only a
    -- driver's can, via the cash-commission mechanism.
    CONSTRAINT wallets_rider_balance_non_negative_check
        CHECK (owner_type <> 'rider' OR balance >= 0)
);

CREATE INDEX wallets_owner_idx
    ON wallets (
        owner_type,
        owner_id
    );

-- +goose Down

DROP TABLE IF EXISTS wallets;

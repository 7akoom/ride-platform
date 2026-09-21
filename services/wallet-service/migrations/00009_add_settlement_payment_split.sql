-- +goose Up

-- How much of a settled trip's fare left the rider's wallet and how much the
-- rider handed to the driver in cash. Both apps need it: the driver to know how
-- much cash to collect, the rider to see what was taken from the wallet.
ALTER TABLE trip_settlements
    ADD COLUMN wallet_amount NUMERIC(16, 3) NOT NULL DEFAULT 0,
    ADD COLUMN cash_amount NUMERIC(16, 3) NOT NULL DEFAULT 0,
    ADD CONSTRAINT trip_settlements_wallet_amount_non_negative_check
        CHECK (wallet_amount >= 0),
    ADD CONSTRAINT trip_settlements_cash_amount_non_negative_check
        CHECK (cash_amount >= 0);

-- Before the split existed a wallet trip was paid entirely from the wallet and a
-- cash trip entirely in cash, so history is exact. A card trip moved neither.
UPDATE trip_settlements SET wallet_amount = fare_amount WHERE payment_method = 'wallet';
UPDATE trip_settlements SET cash_amount = fare_amount WHERE payment_method = 'cash';

-- +goose Down

ALTER TABLE trip_settlements
    DROP CONSTRAINT IF EXISTS trip_settlements_cash_amount_non_negative_check,
    DROP CONSTRAINT IF EXISTS trip_settlements_wallet_amount_non_negative_check,
    DROP COLUMN IF EXISTS cash_amount,
    DROP COLUMN IF EXISTS wallet_amount;

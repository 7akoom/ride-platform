-- +goose Up

-- Starter config. Change per deployment by INSERTing a new row (newest
-- wins). 20% commission matches the common 20-25% industry range.
--
-- suspension_threshold of 50,000 IQD gives a driver a small credit line
-- past zero before their account is suspended — roughly a handful of
-- average fares' worth of commission. Set it to 0 if you want drivers
-- to always be in credit with no grace at all.
INSERT INTO wallet_configs (
    currency_code,
    commission_rate,
    suspension_threshold,
    minimum_payout_amount
) VALUES (
    'IQD',
    20.00,
    50000,
    10000
);

-- +goose Down

DELETE FROM wallet_configs
WHERE currency_code = 'IQD'
  AND commission_rate = 20.00
  AND suspension_threshold = 50000;

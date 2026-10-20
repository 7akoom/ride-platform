-- +goose Up

-- How many days a rider who owes a fee (their wallet could not cover it) may
-- keep requesting trips before it blocks them; past it they also show among
-- the overdue riders for staff, who may suspend the account. 0 blocks at once
-- (the behaviour before this).
ALTER TABLE wallet_configs
    ADD COLUMN dues_grace_days INTEGER NOT NULL DEFAULT 30
        CHECK (dues_grace_days BETWEEN 0 AND 365);

-- +goose Down

ALTER TABLE wallet_configs DROP COLUMN IF EXISTS dues_grace_days;

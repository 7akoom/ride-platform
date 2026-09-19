-- +goose Up

-- How the rider pays. Trips that predate this column, and requests that
-- name none, are cash. card is deliberately not allowed yet: there is no
-- card processor behind it. Adding one means widening this constraint in
-- a migration, adding the constant in the trip package, and teaching
-- wallet-service's settlement about it.
ALTER TABLE trips
    ADD COLUMN payment_method VARCHAR(10) NOT NULL DEFAULT 'cash',
    ADD CONSTRAINT trips_payment_method_check
        CHECK (payment_method IN ('cash', 'wallet'));

-- +goose Down

ALTER TABLE trips
    DROP CONSTRAINT IF EXISTS trips_payment_method_check,
    DROP COLUMN IF EXISTS payment_method;

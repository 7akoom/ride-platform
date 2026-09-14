-- +goose Up

-- One row per settled trip. The UNIQUE constraint on trip_id is what
-- makes SettleTrip idempotent: a retried settlement (network blip, event
-- redelivery) can't pay a driver twice.
CREATE TABLE trip_settlements (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    trip_id UUID NOT NULL,
    rider_id UUID NOT NULL,
    driver_id UUID NOT NULL,

    currency_code VARCHAR(3) NOT NULL,

    payment_method VARCHAR(10) NOT NULL,

    fare_amount NUMERIC(16, 3) NOT NULL,
    commission_rate NUMERIC(5, 2) NOT NULL,
    commission_amount NUMERIC(16, 3) NOT NULL,
    driver_earning NUMERIC(16, 3) NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT trip_settlements_trip_id_unique
        UNIQUE (trip_id),

    CONSTRAINT trip_settlements_payment_method_check
        CHECK (payment_method IN ('cash', 'wallet', 'card')),

    CONSTRAINT trip_settlements_fare_non_negative_check
        CHECK (fare_amount >= 0),

    CONSTRAINT trip_settlements_commission_non_negative_check
        CHECK (commission_amount >= 0)
);

CREATE INDEX trip_settlements_driver_idx
    ON trip_settlements (
        driver_id,
        created_at DESC
    );

-- +goose Down

DROP TABLE IF EXISTS trip_settlements;

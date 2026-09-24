-- +goose Up

-- Staff money operations: a correction of a balance (adjustment), or money
-- given back to a rider for a settled trip (refund), part of which may be
-- taken back from the trip's driver. Each is one row here with the ledger
-- rows it wrote; the creator's key makes a retry the same operation.
CREATE TABLE wallet_adjustments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    kind VARCHAR(12) NOT NULL,

    owner_type VARCHAR(10) NOT NULL,
    owner_id UUID NOT NULL,
    currency_code VARCHAR(3) NOT NULL,
    -- Signed for an adjustment; positive (to the rider) for a refund.
    amount NUMERIC(16, 3) NOT NULL,

    trip_id UUID NULL,
    driver_id UUID NULL,
    driver_amount NUMERIC(16, 3) NOT NULL DEFAULT 0,

    reason VARCHAR(300) NOT NULL,

    transaction_id UUID NOT NULL REFERENCES wallet_transactions (id),
    driver_transaction_id UUID NULL REFERENCES wallet_transactions (id),

    created_by UUID NOT NULL,
    idempotency_key VARCHAR(120) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT wallet_adjustments_creator_key_unique UNIQUE (created_by, idempotency_key),

    CONSTRAINT wallet_adjustments_kind_check CHECK (kind IN ('adjustment', 'refund')),
    CONSTRAINT wallet_adjustments_owner_type_check CHECK (owner_type IN ('rider', 'driver')),
    CONSTRAINT wallet_adjustments_amount_check CHECK (amount <> 0),
    CONSTRAINT wallet_adjustments_refund_check
        CHECK (kind <> 'refund' OR (trip_id IS NOT NULL AND owner_type = 'rider' AND amount > 0)),
    CONSTRAINT wallet_adjustments_driver_amount_check
        CHECK (driver_amount >= 0 AND driver_amount <= abs(amount)),
    CONSTRAINT wallet_adjustments_driver_check
        CHECK (driver_amount = 0 OR (driver_id IS NOT NULL AND driver_transaction_id IS NOT NULL))
);

CREATE INDEX wallet_adjustments_trip_idx
    ON wallet_adjustments (trip_id, created_at)
    WHERE trip_id IS NOT NULL;

CREATE INDEX wallet_adjustments_owner_idx
    ON wallet_adjustments (owner_type, owner_id, created_at DESC);

-- A driver's payout: the amount is held (a payout row leaves the balance when
-- it is asked for), staff pay it outside the platform and mark it paid, or
-- reject it and the held amount comes back.
CREATE TABLE payout_requests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    driver_id UUID NOT NULL,
    currency_code VARCHAR(3) NOT NULL,
    amount NUMERIC(16, 3) NOT NULL,
    destination VARCHAR(120) NOT NULL DEFAULT '',

    status VARCHAR(10) NOT NULL DEFAULT 'pending',

    idempotency_key VARCHAR(120) NOT NULL,
    hold_transaction_id UUID NOT NULL REFERENCES wallet_transactions (id),
    return_transaction_id UUID NULL REFERENCES wallet_transactions (id),

    reviewed_by UUID NULL,
    approved_at TIMESTAMPTZ NULL,
    paid_at TIMESTAMPTZ NULL,
    paid_reference VARCHAR(120) NOT NULL DEFAULT '',
    rejected_at TIMESTAMPTZ NULL,
    reject_reason VARCHAR(300) NOT NULL DEFAULT '',

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT payout_requests_driver_key_unique UNIQUE (driver_id, idempotency_key),

    CONSTRAINT payout_requests_amount_check CHECK (amount > 0),
    CONSTRAINT payout_requests_status_check
        CHECK (status IN ('pending', 'approved', 'paid', 'rejected')),
    CONSTRAINT payout_requests_paid_check
        CHECK (status <> 'paid' OR (paid_at IS NOT NULL AND paid_reference <> '')),
    CONSTRAINT payout_requests_rejected_check
        CHECK (status <> 'rejected' OR (rejected_at IS NOT NULL AND return_transaction_id IS NOT NULL))
);

-- One open request per driver.
CREATE UNIQUE INDEX payout_requests_one_open_per_driver
    ON payout_requests (driver_id)
    WHERE status IN ('pending', 'approved');

CREATE INDEX payout_requests_queue_idx
    ON payout_requests (status, created_at);

CREATE INDEX payout_requests_driver_idx
    ON payout_requests (driver_id, created_at DESC);

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

-- +goose Down

-- The old constraint has neither: those rows keep their amount and become
-- adjustments.
UPDATE wallet_transactions
SET type = 'adjustment'
WHERE type IN ('refund', 'payout_return');

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
                'voucher'
            )
        );

DROP TABLE IF EXISTS payout_requests;
DROP TABLE IF EXISTS wallet_adjustments;

-- +goose Up

-- Cash paid at the company's office and credited by staff against a receipt: a
-- transaction type of its own (cash_top_up), apart from corrections, so the till
-- can be counted (B16). The receipt number is used once in the whole instance.
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
                'incentive',
                'cash_top_up'
            )
        );

ALTER TABLE wallet_adjustments
    ADD COLUMN receipt_number VARCHAR(60) NULL;

ALTER TABLE wallet_adjustments
    DROP CONSTRAINT IF EXISTS wallet_adjustments_kind_check,
    ADD CONSTRAINT wallet_adjustments_kind_check CHECK (kind IN ('adjustment', 'refund', 'cash_top_up')),
    ADD CONSTRAINT wallet_adjustments_cash_top_up_check
        CHECK (kind <> 'cash_top_up' OR (amount > 0 AND receipt_number IS NOT NULL AND trip_id IS NULL));

CREATE UNIQUE INDEX wallet_adjustments_receipt_unique
    ON wallet_adjustments (receipt_number)
    WHERE kind = 'cash_top_up';

CREATE INDEX wallet_adjustments_cash_top_ups_idx
    ON wallet_adjustments (created_at DESC)
    WHERE kind = 'cash_top_up';

-- +goose Down

DROP INDEX IF EXISTS wallet_adjustments_cash_top_ups_idx;
DROP INDEX IF EXISTS wallet_adjustments_receipt_unique;

-- The old constraints have no cash top-up: those become corrections.
UPDATE wallet_adjustments
SET kind = 'adjustment', reason = left('Cash top-up, receipt ' || receipt_number || '. ' || reason, 300)
WHERE kind = 'cash_top_up';

UPDATE wallet_transactions SET type = 'adjustment' WHERE type = 'cash_top_up';

ALTER TABLE wallet_adjustments
    DROP CONSTRAINT IF EXISTS wallet_adjustments_cash_top_up_check,
    DROP CONSTRAINT IF EXISTS wallet_adjustments_kind_check,
    ADD CONSTRAINT wallet_adjustments_kind_check CHECK (kind IN ('adjustment', 'refund')),
    DROP COLUMN IF EXISTS receipt_number;

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

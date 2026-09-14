-- +goose Up

-- An append-only ledger. Rows are never updated or deleted — a mistake
-- is corrected by writing a compensating 'adjustment' row, so the full
-- history of how a balance got to its current value is always
-- reconstructible. balance_after is recorded on every row specifically
-- so the ledger can be audited against the wallet without replaying
-- every transaction.
CREATE TABLE wallet_transactions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    wallet_id UUID NOT NULL REFERENCES wallets (id),

    type VARCHAR(20) NOT NULL,

    -- Signed: negative means money left the wallet.
    amount NUMERIC(16, 3) NOT NULL,
    balance_after NUMERIC(16, 3) NOT NULL,

    trip_id UUID NULL,

    -- Caller-supplied key that makes retries safe: a repeated top-up or
    -- payout with the same key hits the unique index below and is
    -- rejected rather than double-crediting.
    idempotency_key VARCHAR(120) NULL,

    description TEXT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT wallet_transactions_type_check
        CHECK (
            type IN (
                'top_up',
                'trip_payment',
                'trip_earning',
                'commission',
                'payout',
                'adjustment'
            )
        ),

    CONSTRAINT wallet_transactions_amount_non_zero_check
        CHECK (amount <> 0)
);

CREATE UNIQUE INDEX wallet_transactions_idempotency_key_unique
    ON wallet_transactions (
        idempotency_key
    )
    WHERE idempotency_key IS NOT NULL;

CREATE INDEX wallet_transactions_wallet_created_idx
    ON wallet_transactions (
        wallet_id,
        created_at DESC
    );

CREATE INDEX wallet_transactions_trip_idx
    ON wallet_transactions (
        trip_id
    )
    WHERE trip_id IS NOT NULL;

-- +goose Down

DROP TABLE IF EXISTS wallet_transactions;

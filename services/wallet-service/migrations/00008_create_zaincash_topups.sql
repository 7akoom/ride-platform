-- +goose Up

-- Tracks one ZainCash payment attempt from init through to final
-- status. Deliberately separate from wallet_transactions: this table
-- is about talking to ZainCash (idempotency of the /init call, webhook
-- reconciliation), not about the ledger itself. The actual balance
-- credit still goes through wallet.Service.TopUp -> ApplyMovement, the
-- one place money ever moves in this service. A row here reaching
-- 'succeeded' is what triggers that call, using zaincash_transaction_id
-- as the idempotency key so a duplicate webhook can never double-credit.
CREATE TABLE zaincash_topups (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    driver_id UUID NOT NULL,

    -- Our own idempotency key, sent to ZainCash as externalReferenceId
    -- at /init and echoed back as merchantReferenceId in the webhook
    -- and redirect callback payloads.
    external_reference_id UUID NOT NULL UNIQUE DEFAULT gen_random_uuid(),

    -- Not known until the /init call succeeds, so nullable at creation.
    zaincash_transaction_id VARCHAR(64),

    amount NUMERIC(16, 3) NOT NULL,
    currency_code VARCHAR(3) NOT NULL DEFAULT 'IQD',

    status VARCHAR(16) NOT NULL DEFAULT 'pending',

    -- Set once ProcessWebhook actually calls wallet.Service.TopUp for
    -- this row, so a second webhook delivery for the same transaction
    -- is a no-op rather than trying to credit twice.
    credited_at TIMESTAMPTZ,

    failure_reason TEXT,

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT zaincash_topups_amount_positive_check
        CHECK (amount > 0),

    CONSTRAINT zaincash_topups_status_check
        CHECK (status IN ('pending', 'succeeded', 'failed'))
);

CREATE INDEX zaincash_topups_driver_id_idx
    ON zaincash_topups (driver_id);

-- +goose Down

DROP TABLE IF EXISTS zaincash_topups;

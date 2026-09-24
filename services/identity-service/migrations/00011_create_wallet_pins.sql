-- +goose Up

-- The PIN a person confirms money leaving their wallet with. Only a slow
-- hash of it is kept (bcrypt over the identity id and the PIN, so a hash
-- copied onto another identity does not match). Wrong PINs in a row are
-- counted; at the limit the PIN locks until locked_until and lockouts grows,
-- so each lock is longer than the last.
CREATE TABLE wallet_pins (
    identity_id UUID PRIMARY KEY,

    pin_hash VARCHAR(100) NOT NULL,

    failed_attempts SMALLINT NOT NULL DEFAULT 0,
    lockouts SMALLINT NOT NULL DEFAULT 0,
    locked_until TIMESTAMPTZ NULL,

    set_at TIMESTAMPTZ NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT wallet_pins_identity_id_fk
        FOREIGN KEY (identity_id)
        REFERENCES identities (id)
        ON DELETE CASCADE,

    CONSTRAINT wallet_pins_pin_hash_not_blank_check
        CHECK (
            length(btrim(pin_hash)) > 0
        ),

    CONSTRAINT wallet_pins_failed_attempts_check
        CHECK (
            failed_attempts >= 0
        ),

    CONSTRAINT wallet_pins_lockouts_check
        CHECK (
            lockouts >= 0
        )
);


-- +goose Down

DROP TABLE IF EXISTS wallet_pins;

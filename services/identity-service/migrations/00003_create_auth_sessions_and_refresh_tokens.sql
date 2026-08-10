-- +goose Up

CREATE TABLE auth_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    identity_id UUID NOT NULL,

    client_id VARCHAR(100) NULL,
    device_id VARCHAR(255) NULL,
    device_name VARCHAR(255) NULL,
    platform VARCHAR(50) NULL,

    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ NULL,
    last_seen_at TIMESTAMPTZ NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT auth_sessions_identity_id_fk
        FOREIGN KEY (identity_id)
        REFERENCES identities (id)
        ON DELETE CASCADE,

    CONSTRAINT auth_sessions_expiration_check
        CHECK (expires_at > created_at),

    CONSTRAINT auth_sessions_revoked_at_check
        CHECK (
            revoked_at IS NULL
            OR revoked_at >= created_at
        )
);

CREATE INDEX auth_sessions_identity_active_idx
    ON auth_sessions (identity_id, created_at DESC)
    WHERE revoked_at IS NULL;

CREATE INDEX auth_sessions_active_expiration_idx
    ON auth_sessions (expires_at)
    WHERE revoked_at IS NULL;


CREATE TABLE refresh_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    session_id UUID NOT NULL,

    token_hash CHAR(64) NOT NULL,

    expires_at TIMESTAMPTZ NOT NULL,

    used_at TIMESTAMPTZ NULL,
    revoked_at TIMESTAMPTZ NULL,

    replaced_by_token_id UUID NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT refresh_tokens_session_id_fk
        FOREIGN KEY (session_id)
        REFERENCES auth_sessions (id)
        ON DELETE CASCADE,

    CONSTRAINT refresh_tokens_token_hash_unique
        UNIQUE (token_hash),

    CONSTRAINT refresh_tokens_expiration_check
        CHECK (expires_at > created_at),

    CONSTRAINT refresh_tokens_used_at_check
        CHECK (
            used_at IS NULL
            OR (
                used_at >= created_at
                AND used_at <= expires_at
            )
        ),

    CONSTRAINT refresh_tokens_revoked_at_check
        CHECK (
            revoked_at IS NULL
            OR revoked_at >= created_at
        ),

    CONSTRAINT refresh_tokens_replacement_check
        CHECK (
            replaced_by_token_id IS NULL
            OR replaced_by_token_id <> id
        ),

    CONSTRAINT refresh_tokens_replacement_requires_use_check
        CHECK (
            replaced_by_token_id IS NULL
            OR used_at IS NOT NULL
        ),

    CONSTRAINT refresh_tokens_replaced_by_token_id_fk
        FOREIGN KEY (replaced_by_token_id)
        REFERENCES refresh_tokens (id)
        ON DELETE SET NULL
);

CREATE INDEX refresh_tokens_session_created_at_idx
    ON refresh_tokens (session_id, created_at DESC);

CREATE INDEX refresh_tokens_active_expiration_idx
    ON refresh_tokens (expires_at)
    WHERE used_at IS NULL
      AND revoked_at IS NULL;

-- +goose Down

DROP TABLE IF EXISTS refresh_tokens;
DROP TABLE IF EXISTS auth_sessions;
-- +goose Up

CREATE TABLE otp_challenges (
    id VARCHAR(64) PRIMARY KEY,

    phone_number VARCHAR(16) NOT NULL,

    code_hash VARCHAR(64) NOT NULL,

    expires_at TIMESTAMPTZ NOT NULL,
    verified_at TIMESTAMPTZ NULL,

    failed_attempts SMALLINT NOT NULL DEFAULT 0,
    max_attempts SMALLINT NOT NULL DEFAULT 5,

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT otp_challenges_phone_number_format_check
        CHECK (phone_number ~ '^\+[1-9][0-9]{1,14}$'),

    CONSTRAINT otp_challenges_failed_attempts_check
        CHECK (failed_attempts >= 0),

    CONSTRAINT otp_challenges_max_attempts_check
        CHECK (max_attempts > 0),

    CONSTRAINT otp_challenges_attempt_limit_check
        CHECK (failed_attempts <= max_attempts),

    CONSTRAINT otp_challenges_expiration_check
        CHECK (expires_at > created_at),

    CONSTRAINT otp_challenges_verified_at_check
        CHECK (
            verified_at IS NULL
            OR (
                verified_at >= created_at
                AND verified_at <= expires_at
            )
        )
);

CREATE INDEX otp_challenges_phone_created_at_idx
    ON otp_challenges (phone_number, created_at DESC);

CREATE INDEX otp_challenges_active_expiration_idx
    ON otp_challenges (expires_at)
    WHERE verified_at IS NULL;

-- +goose Down

DROP TABLE IF EXISTS otp_challenges;
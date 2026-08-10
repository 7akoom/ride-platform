-- +goose Up

DROP INDEX IF EXISTS otp_challenges_active_expiration_idx;

CREATE INDEX otp_challenges_active_expiration_idx
    ON otp_challenges (expires_at)
    WHERE verified_at IS NULL
      AND cancelled_at IS NULL;

-- +goose Down

DROP INDEX IF EXISTS otp_challenges_active_expiration_idx;

CREATE INDEX otp_challenges_active_expiration_idx
    ON otp_challenges (expires_at)
    WHERE verified_at IS NULL;
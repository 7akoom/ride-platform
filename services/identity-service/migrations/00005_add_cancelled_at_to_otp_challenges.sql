-- +goose Up

ALTER TABLE otp_challenges
ADD COLUMN cancelled_at TIMESTAMPTZ NULL;

ALTER TABLE otp_challenges
ADD CONSTRAINT otp_challenges_cancelled_at_check
CHECK (
    cancelled_at IS NULL
    OR (
        cancelled_at >= created_at
        AND cancelled_at <= expires_at
    )
);

ALTER TABLE otp_challenges
ADD CONSTRAINT otp_challenges_verified_cancelled_exclusive_check
CHECK (
    NOT (
        verified_at IS NOT NULL
        AND cancelled_at IS NOT NULL
    )
);

-- +goose Down

ALTER TABLE otp_challenges
DROP CONSTRAINT IF EXISTS otp_challenges_verified_cancelled_exclusive_check;

ALTER TABLE otp_challenges
DROP CONSTRAINT IF EXISTS otp_challenges_cancelled_at_check;

ALTER TABLE otp_challenges
DROP COLUMN IF EXISTS cancelled_at;
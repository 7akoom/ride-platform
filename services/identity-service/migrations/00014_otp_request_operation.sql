-- +goose Up

-- What a code was asked for when one purpose serves several (signing in, or a
-- signed-in person confirming who they are). The wait between two codes
-- (OTP_REQUEST_COOLDOWN) is kept per operation; the limit per window counts every
-- code to a number. NULL on rows from before: they count as their purpose.
ALTER TABLE otp_request_events ADD COLUMN operation VARCHAR(60) NULL;

CREATE INDEX otp_request_events_identifier_requested_at_idx
    ON otp_request_events (identifier_type, normalized_value, requested_at DESC);

-- +goose Down

DROP INDEX otp_request_events_identifier_requested_at_idx;

ALTER TABLE otp_request_events DROP COLUMN operation;

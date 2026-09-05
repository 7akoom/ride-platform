-- +goose Up

ALTER TABLE otp_request_events
    ADD COLUMN source_ip_address INET NULL;

CREATE INDEX otp_request_events_source_ip_requested_at_idx
    ON otp_request_events (
        source_ip_address,
        requested_at DESC
    )
    WHERE source_ip_address IS NOT NULL;


-- +goose Down

DROP INDEX IF EXISTS otp_request_events_source_ip_requested_at_idx;

ALTER TABLE otp_request_events
    DROP COLUMN IF EXISTS source_ip_address;
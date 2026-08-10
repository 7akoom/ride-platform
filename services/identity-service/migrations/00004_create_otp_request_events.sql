-- +goose Up

CREATE TABLE otp_request_events (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    phone_number VARCHAR(16) NOT NULL,
    requested_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT otp_request_events_phone_number_format_check
        CHECK (phone_number ~ '^\+[1-9][0-9]{1,14}$')
);

CREATE INDEX otp_request_events_phone_requested_at_idx
    ON otp_request_events (
        phone_number,
        requested_at DESC
    );

CREATE INDEX otp_request_events_requested_at_idx
    ON otp_request_events (
        requested_at
    );

-- +goose Down

DROP TABLE IF EXISTS otp_request_events;
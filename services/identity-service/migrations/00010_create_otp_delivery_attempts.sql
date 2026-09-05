-- +goose Up

CREATE TABLE otp_delivery_attempts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    challenge_id VARCHAR(64) NOT NULL,

    channel VARCHAR(20) NOT NULL,
    provider VARCHAR(32) NOT NULL,

    provider_message_id VARCHAR(255) NULL,

    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    last_provider_status VARCHAR(100) NULL,
    failure_code VARCHAR(100) NULL,

    attempted_at TIMESTAMPTZ NOT NULL,
    accepted_at TIMESTAMPTZ NULL,
    sent_at TIMESTAMPTZ NULL,
    delivered_at TIMESTAMPTZ NULL,
    failed_at TIMESTAMPTZ NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT otp_delivery_attempts_challenge_id_not_blank_check
        CHECK (
            length(btrim(challenge_id)) > 0
        ),

    CONSTRAINT otp_delivery_attempts_challenge_id_trimmed_check
        CHECK (
            challenge_id = btrim(challenge_id)
        ),

    CONSTRAINT otp_delivery_attempts_channel_check
        CHECK (
            channel IN (
                'sms',
                'whatsapp',
                'email'
            )
        ),

    CONSTRAINT otp_delivery_attempts_provider_not_blank_check
        CHECK (
            length(btrim(provider)) > 0
        ),

    CONSTRAINT otp_delivery_attempts_provider_trimmed_check
        CHECK (
            provider = btrim(provider)
        ),

    CONSTRAINT otp_delivery_attempts_provider_message_id_not_blank_check
        CHECK (
            provider_message_id IS NULL
            OR length(btrim(provider_message_id)) > 0
        ),

    CONSTRAINT otp_delivery_attempts_provider_message_id_trimmed_check
        CHECK (
            provider_message_id IS NULL
            OR provider_message_id = btrim(provider_message_id)
        ),

    CONSTRAINT otp_delivery_attempts_status_check
        CHECK (
            status IN (
                'pending',
                'accepted',
                'sent',
                'delivered',
                'failed',
                'unknown'
            )
        ),

    CONSTRAINT otp_delivery_attempts_last_provider_status_not_blank_check
        CHECK (
            last_provider_status IS NULL
            OR length(btrim(last_provider_status)) > 0
        ),

    CONSTRAINT otp_delivery_attempts_last_provider_status_trimmed_check
        CHECK (
            last_provider_status IS NULL
            OR last_provider_status = btrim(last_provider_status)
        ),

    CONSTRAINT otp_delivery_attempts_failure_code_not_blank_check
        CHECK (
            failure_code IS NULL
            OR length(btrim(failure_code)) > 0
        ),

    CONSTRAINT otp_delivery_attempts_failure_code_trimmed_check
        CHECK (
            failure_code IS NULL
            OR failure_code = btrim(failure_code)
        ),

    CONSTRAINT otp_delivery_attempts_accepted_at_check
        CHECK (
            accepted_at IS NULL
            OR accepted_at >= attempted_at
        ),

    CONSTRAINT otp_delivery_attempts_sent_at_check
        CHECK (
            sent_at IS NULL
            OR sent_at >= attempted_at
        ),

    CONSTRAINT otp_delivery_attempts_delivered_at_check
        CHECK (
            delivered_at IS NULL
            OR delivered_at >= attempted_at
        ),

    CONSTRAINT otp_delivery_attempts_failed_at_check
        CHECK (
            failed_at IS NULL
            OR failed_at >= attempted_at
        ),

    CONSTRAINT otp_delivery_attempts_delivered_failed_exclusive_check
        CHECK (
            NOT (
                delivered_at IS NOT NULL
                AND failed_at IS NOT NULL
            )
        ),

    CONSTRAINT otp_delivery_attempts_status_timestamp_check
        CHECK (
            (
                status = 'pending'
            )
            OR
            (
                status = 'accepted'
                AND accepted_at IS NOT NULL
            )
            OR
            (
                status = 'sent'
                AND sent_at IS NOT NULL
            )
            OR
            (
                status = 'delivered'
                AND delivered_at IS NOT NULL
            )
            OR
            (
                status = 'failed'
                AND failed_at IS NOT NULL
            )
            OR
            (
                status = 'unknown'
            )
        )
);

CREATE UNIQUE INDEX otp_delivery_attempts_provider_message_id_unique_idx
    ON otp_delivery_attempts (
        provider,
        provider_message_id
    )
    WHERE provider_message_id IS NOT NULL;

CREATE INDEX otp_delivery_attempts_challenge_attempted_at_idx
    ON otp_delivery_attempts (
        challenge_id,
        attempted_at DESC
    );

CREATE INDEX otp_delivery_attempts_provider_status_attempted_at_idx
    ON otp_delivery_attempts (
        provider,
        status,
        attempted_at DESC
    );

CREATE INDEX otp_delivery_attempts_pending_idx
    ON otp_delivery_attempts (
        attempted_at ASC
    )
    WHERE status = 'pending';

CREATE INDEX otp_delivery_attempts_provider_message_lookup_idx
    ON otp_delivery_attempts (
        provider_message_id
    )
    WHERE provider_message_id IS NOT NULL;


-- +goose Down

DROP TABLE IF EXISTS otp_delivery_attempts;
-- +goose Up

CREATE TABLE otp_challenges (
    id VARCHAR(64) PRIMARY KEY,

    identifier_type VARCHAR(20) NOT NULL,
    normalized_value VARCHAR(254) NOT NULL,

    purpose VARCHAR(32) NOT NULL,
    target_identity_id UUID NULL,

    code_hash VARCHAR(64) NOT NULL,

    expires_at TIMESTAMPTZ NOT NULL,
    verified_at TIMESTAMPTZ NULL,
    cancelled_at TIMESTAMPTZ NULL,

    failed_attempts SMALLINT NOT NULL DEFAULT 0,
    max_attempts SMALLINT NOT NULL DEFAULT 5,

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT otp_challenges_target_identity_id_fk
        FOREIGN KEY (target_identity_id)
        REFERENCES identities (id)
        ON DELETE CASCADE,

    CONSTRAINT otp_challenges_identifier_type_check
        CHECK (
            identifier_type IN (
                'phone',
                'email'
            )
        ),

    CONSTRAINT otp_challenges_normalized_value_not_blank_check
        CHECK (
            length(btrim(normalized_value)) > 0
        ),

    CONSTRAINT otp_challenges_normalized_value_trimmed_check
        CHECK (
            normalized_value = btrim(normalized_value)
        ),

    CONSTRAINT otp_challenges_phone_identifier_format_check
        CHECK (
            identifier_type <> 'phone'
            OR normalized_value ~ '^\+[1-9][0-9]{1,14}$'
        ),

    CONSTRAINT otp_challenges_email_identifier_length_check
        CHECK (
            identifier_type <> 'email'
            OR length(normalized_value) <= 254
        ),

    CONSTRAINT otp_challenges_email_identifier_canonical_case_check
        CHECK (
            identifier_type <> 'email'
            OR normalized_value = lower(normalized_value)
        ),

    CONSTRAINT otp_challenges_purpose_check
        CHECK (
            purpose IN (
                'login',
                'link_identifier'
            )
        ),

    CONSTRAINT otp_challenges_purpose_target_check
        CHECK (
            (
                purpose = 'login'
                AND target_identity_id IS NULL
            )
            OR
            (
                purpose = 'link_identifier'
                AND target_identity_id IS NOT NULL
            )
        ),

    CONSTRAINT otp_challenges_failed_attempts_check
        CHECK (
            failed_attempts >= 0
        ),

    CONSTRAINT otp_challenges_max_attempts_check
        CHECK (
            max_attempts > 0
        ),

    CONSTRAINT otp_challenges_attempt_limit_check
        CHECK (
            failed_attempts <= max_attempts
        ),

    CONSTRAINT otp_challenges_expiration_check
        CHECK (
            expires_at > created_at
        ),

    CONSTRAINT otp_challenges_verified_at_check
        CHECK (
            verified_at IS NULL
            OR (
                verified_at >= created_at
                AND verified_at <= expires_at
            )
        ),

    CONSTRAINT otp_challenges_cancelled_at_check
        CHECK (
            cancelled_at IS NULL
            OR (
                cancelled_at >= created_at
                AND cancelled_at <= expires_at
            )
        ),

    CONSTRAINT otp_challenges_verified_cancelled_exclusive_check
        CHECK (
            NOT (
                verified_at IS NOT NULL
                AND cancelled_at IS NOT NULL
            )
        )
);

CREATE INDEX otp_challenges_active_identifier_idx
    ON otp_challenges (
        identifier_type,
        normalized_value,
        purpose,
        target_identity_id,
        created_at DESC
    )
    WHERE verified_at IS NULL
      AND cancelled_at IS NULL;

CREATE INDEX otp_challenges_active_expiration_idx
    ON otp_challenges (
        expires_at
    )
    WHERE verified_at IS NULL
      AND cancelled_at IS NULL;

CREATE INDEX otp_challenges_target_identity_id_idx
    ON otp_challenges (
        target_identity_id
    )
    WHERE target_identity_id IS NOT NULL;


CREATE TABLE otp_request_events (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,

    identifier_type VARCHAR(20) NOT NULL,
    normalized_value VARCHAR(254) NOT NULL,

    purpose VARCHAR(32) NOT NULL,
    target_identity_id UUID NULL,

    requested_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT otp_request_events_target_identity_id_fk
        FOREIGN KEY (target_identity_id)
        REFERENCES identities (id)
        ON DELETE CASCADE,

    CONSTRAINT otp_request_events_identifier_type_check
        CHECK (
            identifier_type IN (
                'phone',
                'email'
            )
        ),

    CONSTRAINT otp_request_events_normalized_value_not_blank_check
        CHECK (
            length(btrim(normalized_value)) > 0
        ),

    CONSTRAINT otp_request_events_normalized_value_trimmed_check
        CHECK (
            normalized_value = btrim(normalized_value)
        ),

    CONSTRAINT otp_request_events_phone_identifier_format_check
        CHECK (
            identifier_type <> 'phone'
            OR normalized_value ~ '^\+[1-9][0-9]{1,14}$'
        ),

    CONSTRAINT otp_request_events_email_identifier_length_check
        CHECK (
            identifier_type <> 'email'
            OR length(normalized_value) <= 254
        ),

    CONSTRAINT otp_request_events_email_identifier_canonical_case_check
        CHECK (
            identifier_type <> 'email'
            OR normalized_value = lower(normalized_value)
        ),

    CONSTRAINT otp_request_events_purpose_check
        CHECK (
            purpose IN (
                'login',
                'link_identifier'
            )
        ),

    CONSTRAINT otp_request_events_purpose_target_check
        CHECK (
            (
                purpose = 'login'
                AND target_identity_id IS NULL
            )
            OR
            (
                purpose = 'link_identifier'
                AND target_identity_id IS NOT NULL
            )
        )
);

CREATE INDEX otp_request_events_scope_requested_at_idx
    ON otp_request_events (
        identifier_type,
        normalized_value,
        purpose,
        target_identity_id,
        requested_at DESC
    );

CREATE INDEX otp_request_events_requested_at_idx
    ON otp_request_events (
        requested_at
    );

CREATE INDEX otp_request_events_target_identity_id_idx
    ON otp_request_events (
        target_identity_id
    )
    WHERE target_identity_id IS NOT NULL;


-- +goose Down

DROP TABLE IF EXISTS otp_request_events;
DROP TABLE IF EXISTS otp_challenges;
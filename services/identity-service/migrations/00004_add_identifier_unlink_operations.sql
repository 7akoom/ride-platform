-- +goose Up

ALTER TABLE otp_challenges
    DROP CONSTRAINT otp_challenges_purpose_target_check,
    DROP CONSTRAINT otp_challenges_purpose_check;

ALTER TABLE otp_challenges
    ADD CONSTRAINT otp_challenges_purpose_check
        CHECK (
            purpose IN (
                'login',
                'link_identifier',
                'unlink_identifier'
            )
        ),
    ADD CONSTRAINT otp_challenges_purpose_target_check
        CHECK (
            (
                purpose = 'login'
                AND target_identity_id IS NULL
            )
            OR
            (
                purpose IN (
                    'link_identifier',
                    'unlink_identifier'
                )
                AND target_identity_id IS NOT NULL
            )
        );


ALTER TABLE otp_request_events
    DROP CONSTRAINT otp_request_events_purpose_target_check,
    DROP CONSTRAINT otp_request_events_purpose_check;

ALTER TABLE otp_request_events
    ADD CONSTRAINT otp_request_events_purpose_check
        CHECK (
            purpose IN (
                'login',
                'link_identifier',
                'unlink_identifier'
            )
        ),
    ADD CONSTRAINT otp_request_events_purpose_target_check
        CHECK (
            (
                purpose = 'login'
                AND target_identity_id IS NULL
            )
            OR
            (
                purpose IN (
                    'link_identifier',
                    'unlink_identifier'
                )
                AND target_identity_id IS NOT NULL
            )
        );


CREATE TABLE identifier_unlink_operations (
    challenge_id VARCHAR(64) PRIMARY KEY,

    identity_id UUID NOT NULL,

    identifier_type VARCHAR(20) NOT NULL,
    normalized_value VARCHAR(254) NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT identifier_unlink_operations_challenge_id_fk
        FOREIGN KEY (challenge_id)
        REFERENCES otp_challenges (id)
        ON DELETE CASCADE,

    CONSTRAINT identifier_unlink_operations_identity_id_fk
        FOREIGN KEY (identity_id)
        REFERENCES identities (id)
        ON DELETE CASCADE,

    CONSTRAINT identifier_unlink_operations_identifier_type_check
        CHECK (
            identifier_type IN (
                'phone',
                'email'
            )
        ),

    CONSTRAINT identifier_unlink_operations_value_not_blank_check
        CHECK (
            length(btrim(normalized_value)) > 0
        ),

    CONSTRAINT identifier_unlink_operations_value_trimmed_check
        CHECK (
            normalized_value = btrim(normalized_value)
        ),

    CONSTRAINT identifier_unlink_operations_phone_format_check
        CHECK (
            identifier_type <> 'phone'
            OR normalized_value ~ '^\+[1-9][0-9]{1,14}$'
        ),

    CONSTRAINT identifier_unlink_operations_email_length_check
        CHECK (
            identifier_type <> 'email'
            OR length(normalized_value) <= 254
        ),

    CONSTRAINT identifier_unlink_operations_email_canonical_case_check
        CHECK (
            identifier_type <> 'email'
            OR normalized_value = lower(normalized_value)
        )
);

CREATE INDEX identifier_unlink_operations_identity_identifier_idx
    ON identifier_unlink_operations (
        identity_id,
        identifier_type,
        normalized_value,
        created_at DESC
    );


-- +goose Down

DELETE FROM otp_request_events
WHERE purpose = 'unlink_identifier';

DELETE FROM otp_challenges
WHERE purpose = 'unlink_identifier';

DROP TABLE IF EXISTS identifier_unlink_operations;


ALTER TABLE otp_request_events
    DROP CONSTRAINT otp_request_events_purpose_target_check,
    DROP CONSTRAINT otp_request_events_purpose_check;

ALTER TABLE otp_request_events
    ADD CONSTRAINT otp_request_events_purpose_check
        CHECK (
            purpose IN (
                'login',
                'link_identifier'
            )
        ),
    ADD CONSTRAINT otp_request_events_purpose_target_check
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
        );


ALTER TABLE otp_challenges
    DROP CONSTRAINT otp_challenges_purpose_target_check,
    DROP CONSTRAINT otp_challenges_purpose_check;

ALTER TABLE otp_challenges
    ADD CONSTRAINT otp_challenges_purpose_check
        CHECK (
            purpose IN (
                'login',
                'link_identifier'
            )
        ),
    ADD CONSTRAINT otp_challenges_purpose_target_check
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
        );
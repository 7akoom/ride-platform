-- +goose Up

-- A person deleting their account confirms it with a code sent to one of
-- their own sign-in methods: a new OTP purpose, always for an identity.
ALTER TABLE otp_challenges
    DROP CONSTRAINT otp_challenges_purpose_target_check,
    DROP CONSTRAINT otp_challenges_purpose_check;

ALTER TABLE otp_challenges
    ADD CONSTRAINT otp_challenges_purpose_check
        CHECK (
            purpose IN (
                'login',
                'link_identifier',
                'unlink_identifier',
                'delete_account'
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
                    'unlink_identifier',
                    'delete_account'
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
                'unlink_identifier',
                'delete_account'
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
                    'unlink_identifier',
                    'delete_account'
                )
                AND target_identity_id IS NOT NULL
            )
        );

-- One row per identity: its latest deletion. pending until purge_after (the
-- grace period), cancelled by signing in, completed once erased. The rider and
-- driver profiles are noted when asked, so the services holding them can be
-- told without asking again.
CREATE TABLE account_deletions (
    identity_id UUID PRIMARY KEY
        REFERENCES identities (id)
        ON DELETE CASCADE,

    status VARCHAR(16) NOT NULL,

    rider_id UUID NULL,
    driver_id UUID NULL,

    -- The person accepted losing a positive wallet balance.
    balance_loss_accepted BOOLEAN NOT NULL DEFAULT false,

    requested_at TIMESTAMPTZ NOT NULL,
    purge_after TIMESTAMPTZ NOT NULL,
    cancelled_at TIMESTAMPTZ NULL,
    completed_at TIMESTAMPTZ NULL,

    -- The eraser tries again from here after something stood in the way
    -- (a service not answering, a trip that started in the last minutes).
    next_attempt_at TIMESTAMPTZ NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error VARCHAR(300) NOT NULL DEFAULT '',

    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT account_deletions_status_check
        CHECK (status IN ('pending', 'cancelled', 'completed')),

    CONSTRAINT account_deletions_purge_check
        CHECK (purge_after >= requested_at),

    CONSTRAINT account_deletions_cancelled_check
        CHECK ((status = 'cancelled') = (cancelled_at IS NOT NULL)),

    CONSTRAINT account_deletions_completed_check
        CHECK ((status = 'completed') = (completed_at IS NOT NULL))
);

CREATE INDEX account_deletions_due_idx
    ON account_deletions (purge_after)
    WHERE status = 'pending';

-- +goose Down

DROP TABLE IF EXISTS account_deletions;

DELETE FROM otp_request_events WHERE purpose = 'delete_account';
DELETE FROM otp_challenges WHERE purpose = 'delete_account';

ALTER TABLE otp_challenges
    DROP CONSTRAINT otp_challenges_purpose_target_check,
    DROP CONSTRAINT otp_challenges_purpose_check;

ALTER TABLE otp_challenges
    ADD CONSTRAINT otp_challenges_purpose_check
        CHECK (purpose IN ('login', 'link_identifier', 'unlink_identifier')),
    ADD CONSTRAINT otp_challenges_purpose_target_check
        CHECK (
            (purpose = 'login' AND target_identity_id IS NULL)
            OR (purpose IN ('link_identifier', 'unlink_identifier') AND target_identity_id IS NOT NULL)
        );

ALTER TABLE otp_request_events
    DROP CONSTRAINT otp_request_events_purpose_target_check,
    DROP CONSTRAINT otp_request_events_purpose_check;

ALTER TABLE otp_request_events
    ADD CONSTRAINT otp_request_events_purpose_check
        CHECK (purpose IN ('login', 'link_identifier', 'unlink_identifier')),
    ADD CONSTRAINT otp_request_events_purpose_target_check
        CHECK (
            (purpose = 'login' AND target_identity_id IS NULL)
            OR (purpose IN ('link_identifier', 'unlink_identifier') AND target_identity_id IS NOT NULL)
        );

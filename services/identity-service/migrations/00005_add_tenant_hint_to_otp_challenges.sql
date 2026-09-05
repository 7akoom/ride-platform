-- +goose Up

ALTER TABLE otp_challenges
    ADD COLUMN tenant_hint VARCHAR(128) NULL;

ALTER TABLE otp_challenges
    ADD CONSTRAINT otp_challenges_tenant_hint_not_blank_check
    CHECK (
        tenant_hint IS NULL
        OR length(btrim(tenant_hint)) > 0
    );

ALTER TABLE otp_challenges
    ADD CONSTRAINT otp_challenges_tenant_hint_trimmed_check
    CHECK (
        tenant_hint IS NULL
        OR tenant_hint = btrim(tenant_hint)
    );

DROP INDEX otp_challenges_active_identifier_idx;

CREATE INDEX otp_challenges_active_identifier_idx
    ON otp_challenges (
        tenant_hint,
        identifier_type,
        normalized_value,
        purpose,
        target_identity_id,
        created_at DESC
    )
    WHERE verified_at IS NULL
      AND cancelled_at IS NULL;

-- +goose Down

DROP INDEX otp_challenges_active_identifier_idx;

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

ALTER TABLE otp_challenges
    DROP CONSTRAINT otp_challenges_tenant_hint_trimmed_check;

ALTER TABLE otp_challenges
    DROP CONSTRAINT otp_challenges_tenant_hint_not_blank_check;

ALTER TABLE otp_challenges
    DROP COLUMN tenant_hint;
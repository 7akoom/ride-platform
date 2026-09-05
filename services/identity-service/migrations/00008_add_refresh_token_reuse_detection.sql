-- +goose Up

ALTER TABLE refresh_tokens
    ADD COLUMN reuse_detected_at TIMESTAMPTZ NULL;

ALTER TABLE refresh_tokens
    ADD CONSTRAINT refresh_tokens_reuse_detected_at_check
    CHECK (
        reuse_detected_at IS NULL
        OR reuse_detected_at >= created_at
    );

ALTER TABLE refresh_tokens
    ADD CONSTRAINT refresh_tokens_reuse_requires_use_check
    CHECK (
        reuse_detected_at IS NULL
        OR used_at IS NOT NULL
    );


-- +goose Down

ALTER TABLE refresh_tokens
    DROP CONSTRAINT refresh_tokens_reuse_requires_use_check;

ALTER TABLE refresh_tokens
    DROP CONSTRAINT refresh_tokens_reuse_detected_at_check;

ALTER TABLE refresh_tokens
    DROP COLUMN reuse_detected_at;
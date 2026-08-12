-- +goose Up

ALTER TABLE refresh_tokens
ADD CONSTRAINT refresh_tokens_token_hash_format_check
CHECK (
    token_hash ~ '^[0-9a-f]{64}$'
);

-- +goose Down

ALTER TABLE refresh_tokens
DROP CONSTRAINT IF EXISTS refresh_tokens_token_hash_format_check;
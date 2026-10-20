-- +goose Up

-- The values a notification's template was filled with ({ticket}, {amount}...), so the
-- inbox can show it again in the language the person reads it in now, not only the
-- one it was written in. NULL for notifications from before: they keep their text.
ALTER TABLE notifications ADD COLUMN variables JSONB;

-- +goose Down

ALTER TABLE notifications DROP COLUMN variables;

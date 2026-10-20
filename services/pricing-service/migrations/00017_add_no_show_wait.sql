-- +goose Up

-- How long a driver waits at the pickup before they may report that the
-- rider did not come (and the no-show fee applies). On the card, beside the
-- free waiting minutes and the fee, so staff set it with the prices.
ALTER TABLE pricing_configs
    ADD COLUMN no_show_wait_minutes INTEGER NOT NULL DEFAULT 6
        CHECK (no_show_wait_minutes BETWEEN 1 AND 30);

-- +goose Down

ALTER TABLE pricing_configs DROP COLUMN IF EXISTS no_show_wait_minutes;

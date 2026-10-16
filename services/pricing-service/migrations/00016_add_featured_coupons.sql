-- +goose Up

-- A featured coupon is shown to riders among the offers on the app's home
-- (GET /v1/riders/{id}/offers); the others are typed in only.
ALTER TABLE coupons
    ADD COLUMN featured BOOLEAN NOT NULL DEFAULT false;

CREATE INDEX coupons_featured_idx ON coupons (created_at DESC, id) WHERE featured AND active;

-- +goose Down

DROP INDEX IF EXISTS coupons_featured_idx;

ALTER TABLE coupons
    DROP COLUMN IF EXISTS featured;

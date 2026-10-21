-- +goose Up

-- When a featured coupon was announced to riders by push (fare.offer.featured),
-- so it is announced once. Coupons featured before this are taken as announced:
-- they are not pushed now.
ALTER TABLE coupons ADD COLUMN announced_at TIMESTAMPTZ NULL;

UPDATE coupons SET announced_at = CURRENT_TIMESTAMP WHERE featured;

-- +goose Down

ALTER TABLE coupons DROP COLUMN IF EXISTS announced_at;

-- +goose Up

-- Coupons get a description for staff, a cap on a percentage's discount,
-- where and for which classes they apply, an option for riders who never
-- completed a trip, and who created and last changed them.
ALTER TABLE coupons
    ADD COLUMN description VARCHAR(200) NOT NULL DEFAULT '',
    ADD COLUMN max_discount_amount NUMERIC(12, 4) NULL,
    ADD COLUMN city_id UUID NULL,
    ADD COLUMN zone_id UUID NULL,
    ADD COLUMN vehicle_classes TEXT[] NOT NULL DEFAULT '{}',
    ADD COLUMN new_riders_only BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN created_by UUID NULL,
    ADD COLUMN updated_by UUID NULL,
    ADD CONSTRAINT coupons_max_discount_positive_check
        CHECK (max_discount_amount IS NULL OR max_discount_amount > 0),
    ADD CONSTRAINT coupons_one_place_check
        CHECK (city_id IS NULL OR zone_id IS NULL);

CREATE INDEX coupons_created_at_idx ON coupons (created_at DESC, id);

-- A claimed quote holds its coupon (reserved) until the trip completes
-- (redeemed) or is cancelled or never created (released). redemption_count
-- on the coupon counts the ones held: reserved and redeemed.
ALTER TABLE coupon_redemptions
    ADD COLUMN status VARCHAR(10) NOT NULL DEFAULT 'redeemed',
    ADD COLUMN quote_id UUID NULL,
    ADD COLUMN released_at TIMESTAMPTZ NULL,
    ADD CONSTRAINT coupon_redemptions_status_check
        CHECK (status IN ('reserved', 'redeemed', 'released'));

-- A trip holds at most one coupon.
CREATE UNIQUE INDEX coupon_redemptions_trip_held_idx
    ON coupon_redemptions (trip_id)
    WHERE trip_id IS NOT NULL AND status <> 'released';

CREATE INDEX coupon_redemptions_coupon_time_idx
    ON coupon_redemptions (coupon_id, redeemed_at DESC, id);

UPDATE coupons
SET redemption_count = (
    SELECT COUNT(*) FROM coupon_redemptions r
    WHERE r.coupon_id = coupons.id AND r.status <> 'released'
);

-- The automatic discounts: on a rider's first trip, and on every Nth one.
-- One row. A percent of 0 (or loyalty_every 0) turns that discount off; a
-- max amount caps it.
CREATE TABLE promotion_settings (
    id BOOLEAN PRIMARY KEY DEFAULT true,

    first_ride_percent NUMERIC(5, 2) NOT NULL DEFAULT 50,
    first_ride_max_amount NUMERIC(12, 4) NULL,

    loyalty_every INTEGER NOT NULL DEFAULT 10,
    loyalty_percent NUMERIC(5, 2) NOT NULL DEFAULT 20,
    loyalty_max_amount NUMERIC(12, 4) NULL,

    updated_by UUID NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT promotion_settings_one_row_check CHECK (id),
    CONSTRAINT promotion_settings_first_ride_percent_check
        CHECK (first_ride_percent >= 0 AND first_ride_percent <= 100),
    CONSTRAINT promotion_settings_loyalty_percent_check
        CHECK (loyalty_percent >= 0 AND loyalty_percent <= 100),
    CONSTRAINT promotion_settings_loyalty_every_check
        CHECK (loyalty_every = 0 OR (loyalty_every >= 2 AND loyalty_every <= 100)),
    CONSTRAINT promotion_settings_first_ride_max_check
        CHECK (first_ride_max_amount IS NULL OR first_ride_max_amount > 0),
    CONSTRAINT promotion_settings_loyalty_max_check
        CHECK (loyalty_max_amount IS NULL OR loyalty_max_amount > 0)
);

INSERT INTO promotion_settings (id) VALUES (true);

-- +goose Down

DROP TABLE IF EXISTS promotion_settings;

DROP INDEX IF EXISTS coupon_redemptions_coupon_time_idx;
DROP INDEX IF EXISTS coupon_redemptions_trip_held_idx;

-- Released holds were never uses.
DELETE FROM coupon_redemptions WHERE status = 'released';

ALTER TABLE coupon_redemptions
    DROP CONSTRAINT IF EXISTS coupon_redemptions_status_check,
    DROP COLUMN IF EXISTS released_at,
    DROP COLUMN IF EXISTS quote_id,
    DROP COLUMN IF EXISTS status;

DROP INDEX IF EXISTS coupons_created_at_idx;

ALTER TABLE coupons
    DROP CONSTRAINT IF EXISTS coupons_one_place_check,
    DROP CONSTRAINT IF EXISTS coupons_max_discount_positive_check,
    DROP COLUMN IF EXISTS updated_by,
    DROP COLUMN IF EXISTS created_by,
    DROP COLUMN IF EXISTS new_riders_only,
    DROP COLUMN IF EXISTS vehicle_classes,
    DROP COLUMN IF EXISTS zone_id,
    DROP COLUMN IF EXISTS city_id,
    DROP COLUMN IF EXISTS max_discount_amount,
    DROP COLUMN IF EXISTS description;

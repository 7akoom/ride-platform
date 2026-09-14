-- +goose Up

CREATE TABLE coupon_redemptions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    coupon_id UUID NOT NULL REFERENCES coupons (id),
    rider_id UUID NOT NULL,
    trip_id UUID NULL,

    discount_amount NUMERIC(12, 4) NOT NULL,

    redeemed_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT coupon_redemptions_discount_amount_non_negative_check
        CHECK (discount_amount >= 0)
);

CREATE INDEX coupon_redemptions_coupon_rider_idx
    ON coupon_redemptions (
        coupon_id,
        rider_id
    );

-- +goose Down

DROP TABLE IF EXISTS coupon_redemptions;

-- +goose Up

CREATE TABLE coupons (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    code VARCHAR(40) NOT NULL,

    discount_type VARCHAR(20) NOT NULL,
    discount_value NUMERIC(12, 4) NOT NULL,

    valid_from TIMESTAMPTZ NOT NULL,
    valid_until TIMESTAMPTZ NOT NULL,

    -- NULL means unlimited total redemptions.
    max_redemptions INTEGER NULL,
    redemption_count INTEGER NOT NULL DEFAULT 0,

    per_rider_limit INTEGER NOT NULL DEFAULT 1,

    minimum_fare_amount NUMERIC(12, 4) NOT NULL DEFAULT 0,

    active BOOLEAN NOT NULL DEFAULT true,

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT coupons_code_unique
        UNIQUE (code),

    CONSTRAINT coupons_discount_type_check
        CHECK (discount_type IN ('percentage', 'fixed_amount')),

    CONSTRAINT coupons_discount_value_positive_check
        CHECK (discount_value > 0),

    CONSTRAINT coupons_percentage_range_check
        CHECK (discount_type <> 'percentage' OR discount_value <= 100),

    CONSTRAINT coupons_valid_range_check
        CHECK (valid_until > valid_from),

    CONSTRAINT coupons_max_redemptions_positive_check
        CHECK (max_redemptions IS NULL OR max_redemptions > 0),

    CONSTRAINT coupons_redemption_count_non_negative_check
        CHECK (redemption_count >= 0),

    CONSTRAINT coupons_per_rider_limit_positive_check
        CHECK (per_rider_limit > 0),

    CONSTRAINT coupons_minimum_fare_non_negative_check
        CHECK (minimum_fare_amount >= 0)
);

-- +goose Down

DROP TABLE IF EXISTS coupons;

-- +goose Up

-- How long a driver has to accept an offer, set by staff with the other dispatch
-- settings. 0 keeps the instance's own default (DISPATCH_OFFER_TTL of
-- dispatch-service), as every instance had until now.
ALTER TABLE dispatch_settings
    ADD COLUMN offer_seconds INTEGER NOT NULL DEFAULT 0,
    ADD CONSTRAINT dispatch_settings_offer_seconds_check
        CHECK (offer_seconds = 0 OR offer_seconds BETWEEN 5 AND 60);

-- +goose Down

ALTER TABLE dispatch_settings
    DROP CONSTRAINT IF EXISTS dispatch_settings_offer_seconds_check,
    DROP COLUMN IF EXISTS offer_seconds;

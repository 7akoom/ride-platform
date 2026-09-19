-- +goose Up

-- NULL vehicle_class = a rate card that applies to any class. A non-NULL
-- value is a rate card specific to that class. Selection order for a
-- fare, most specific first: (zone, class), (zone, any class),
-- (no zone, class), then the deployment-wide default. Existing rows keep
-- NULL, so every rate card that predates vehicle classes keeps working.
ALTER TABLE pricing_configs
    ADD COLUMN vehicle_class VARCHAR(20),
    ADD CONSTRAINT pricing_configs_vehicle_class_check
        CHECK (vehicle_class IS NULL OR vehicle_class IN ('economy', 'comfort'));

CREATE INDEX pricing_configs_zone_class_created_at_idx
    ON pricing_configs (zone_id, vehicle_class, created_at DESC);

-- Audit trail: which class a persisted fare was priced for. Old fares
-- stay NULL (they were priced before classes existed).
ALTER TABLE fares
    ADD COLUMN vehicle_class VARCHAR(20);

-- +goose Down

ALTER TABLE fares DROP COLUMN vehicle_class;

DROP INDEX IF EXISTS pricing_configs_zone_class_created_at_idx;
ALTER TABLE pricing_configs
    DROP CONSTRAINT IF EXISTS pricing_configs_vehicle_class_check,
    DROP COLUMN vehicle_class;

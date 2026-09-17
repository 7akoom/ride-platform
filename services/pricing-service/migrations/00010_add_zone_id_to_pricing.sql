-- +goose Up

-- NULL zone_id = the deployment's global default rate card. A non-NULL
-- zone_id is a rate card specific to one of location-service's service
-- zones (see that service's `zones` table) — a per-zone config that
-- exists takes priority over the global default when pricing a pickup
-- inside that zone; a zone with no config of its own falls back to the
-- global default. No FK to location-service's own database (services
-- never reach into each other's tables) — zone_id is just an opaque
-- reference, validated indirectly via location-service's
-- CheckServiceZone RPC before it's ever used here.
ALTER TABLE pricing_configs
    ADD COLUMN zone_id UUID;

CREATE INDEX pricing_configs_zone_id_created_at_idx
    ON pricing_configs (zone_id, created_at DESC);

-- Records which zone (if any) a persisted fare was actually priced
-- under, for audit/debugging — independent of whether that zone had
-- its own rate card or fell back to the global default.
ALTER TABLE fares
    ADD COLUMN zone_id UUID;

CREATE INDEX fares_zone_id_idx ON fares (zone_id);

-- +goose Down

DROP INDEX IF EXISTS fares_zone_id_idx;
ALTER TABLE fares DROP COLUMN zone_id;

DROP INDEX IF EXISTS pricing_configs_zone_id_created_at_idx;
ALTER TABLE pricing_configs DROP COLUMN zone_id;

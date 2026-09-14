-- +goose Up

-- Starter values so the service is usable immediately after migrating.
-- These are placeholders priced for IQD — change CURRENCY_CODE and the
-- rates to match the deployment by INSERTing a new pricing_configs row
-- (the newest row wins; old rows stay as history).
INSERT INTO pricing_configs (
    currency_code,
    base_fare,
    per_km_rate,
    per_minute_rate,
    average_speed_kmh,
    distance_correction_factor
) VALUES (
    'IQD',
    2000,
    500,
    50,
    30,
    1.3
);

-- Morning and evening rush hours, every day. Adjust or deactivate per
-- deployment; overlapping rules resolve to the highest match, never a sum.
INSERT INTO surge_time_rules (label, day_of_week, start_time, end_time, surge_percent)
VALUES
    ('Morning rush', NULL, '07:00:00', '09:00:00', 25),
    ('Evening rush', NULL, '16:00:00', '19:00:00', 30),
    ('Late night', NULL, '23:00:00', '04:00:00', 20);

-- +goose Down

DELETE FROM surge_time_rules
WHERE label IN ('Morning rush', 'Evening rush', 'Late night');

DELETE FROM pricing_configs
WHERE currency_code = 'IQD'
  AND base_fare = 2000
  AND per_km_rate = 500;

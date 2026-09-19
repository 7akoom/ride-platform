package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/pricing"
)

const activeConfigSelectSQL = `SELECT id, COALESCE(zone_id::text, ''), COALESCE(vehicle_class, ''),
        currency_code, base_fare, per_km_rate, per_minute_rate,
        average_speed_kmh, distance_correction_factor, created_at
        FROM pricing_configs`

// GetActiveConfig picks the rate card for a fare in one query. A card
// matches when its zone_id is the requested zone or NULL and its
// vehicle_class is the requested class or NULL. Among the matches, the
// most specific wins, then the newest (rows are versioned, latest is
// active): (zone, class), then (zone, any class), then (no zone, class),
// then the deployment-wide default. zoneID is empty only when there is no
// zone to match, in which case only zone-less cards are candidates.
func (r *PricingRepository) GetActiveConfig(
	ctx context.Context,
	zoneID, vehicleClass string,
) (pricing.Config, error) {
	row := r.pool.QueryRow(
		ctx,
		activeConfigSelectSQL+`
		 WHERE (zone_id = $1::uuid OR zone_id IS NULL)
		   AND (vehicle_class = $2::text OR vehicle_class IS NULL)
		 ORDER BY (zone_id IS NOT NULL) DESC,
		          (vehicle_class IS NOT NULL) DESC,
		          created_at DESC
		 LIMIT 1`,
		nullableUUID(zoneID),
		vehicleClass,
	)

	var config pricing.Config

	err := row.Scan(
		&config.ID,
		&config.ZoneID,
		&config.VehicleClass,
		&config.CurrencyCode,
		&config.BaseFare,
		&config.PerKmRate,
		&config.PerMinuteRate,
		&config.AverageSpeedKmh,
		&config.DistanceCorrectionFactor,
		&config.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return pricing.Config{}, pricing.ErrNoActiveConfig
		}

		return pricing.Config{}, fmt.Errorf("select active pricing config: %w", err)
	}

	return config, nil
}

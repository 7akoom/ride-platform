package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/pricing"
)

// configColumns is every rate card column, in the order scanConfig reads
// them.
const configColumns = `id, COALESCE(zone_id::text, ''), COALESCE(city_id::text, ''), COALESCE(vehicle_class, ''),
        currency_code, base_fare, per_km_rate, per_minute_rate, minimum_fare,
        free_waiting_minutes, waiting_per_minute, cancellation_fee,
        cancellation_grace_minutes, no_show_fee,
        max_surge_percent, demand_surge, weather_surge,
        average_speed_kmh, distance_correction_factor,
        retired, COALESCE(created_by::text, ''), created_at`

// currentConfigsSQL is the newest version of each place's and class's card
// (retired ones included: a retired newest version is how a place loses its
// card). DISTINCT ON treats NULLs as equal, so every "everywhere" or "every
// class" card groups with its own versions.
const currentConfigsSQL = `SELECT DISTINCT ON (zone_id, city_id, vehicle_class) ` + configColumns + `
        FROM pricing_configs`

const currentConfigsOrder = `
        ORDER BY zone_id, city_id, vehicle_class, created_at DESC, id`

// GetActiveConfig picks the rate card for a fare. A card matches when its
// zone is the pickup's or none, its city is the pickup's or none, and its
// class is the fare's or none; each place and class counts with its newest
// version only, and not when that version is retired. Among the matches the
// most specific wins: zone, then city, then everywhere; within each, the
// class's own card before the one for every class.
func (r *PricingRepository) GetActiveConfig(
	ctx context.Context,
	scope pricing.Scope,
) (pricing.Config, error) {
	rows, err := r.pool.Query(
		ctx,
		currentConfigsSQL+`
		 WHERE (zone_id = $1::uuid OR zone_id IS NULL)
		   AND (city_id = $2::uuid OR city_id IS NULL)
		   AND (vehicle_class = $3::text OR vehicle_class IS NULL)`+currentConfigsOrder,
		nullableUUID(scope.ZoneID),
		nullableUUID(scope.CityID),
		scope.VehicleClass,
	)
	if err != nil {
		return pricing.Config{}, fmt.Errorf("select pricing configs: %w", err)
	}

	candidates, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (pricing.Config, error) {
		return scanConfig(row)
	})
	if err != nil {
		return pricing.Config{}, fmt.Errorf("read pricing configs: %w", err)
	}

	best, found := mostSpecific(candidates)
	if !found {
		return pricing.Config{}, pricing.ErrNoActiveConfig
	}

	return best, nil
}

// mostSpecific is the card in force among current versions that all match
// the fare.
func mostSpecific(candidates []pricing.Config) (pricing.Config, bool) {
	var (
		best      pricing.Config
		bestScore = -1
	)

	for _, card := range candidates {
		if card.Retired {
			continue
		}

		score := 0

		switch {
		case card.ZoneID != "":
			score += 4
		case card.CityID != "":
			score += 2
		}

		if card.VehicleClass != "" {
			score++
		}

		if score > bestScore {
			best, bestScore = card, score
		}
	}

	return best, bestScore >= 0
}

func scanConfig(row pgx.Row) (pricing.Config, error) {
	var config pricing.Config

	err := row.Scan(
		&config.ID,
		&config.ZoneID,
		&config.CityID,
		&config.VehicleClass,
		&config.CurrencyCode,
		&config.BaseFare,
		&config.PerKmRate,
		&config.PerMinuteRate,
		&config.MinimumFare,
		&config.FreeWaitingMinutes,
		&config.WaitingPerMinute,
		&config.CancellationFee,
		&config.CancellationGraceMinutes,
		&config.NoShowFee,
		&config.MaxSurgePercent,
		&config.DemandSurge,
		&config.WeatherSurge,
		&config.AverageSpeedKmh,
		&config.DistanceCorrectionFactor,
		&config.Retired,
		&config.CreatedBy,
		&config.CreatedAt,
	)
	if err != nil {
		return pricing.Config{}, err
	}

	return config, nil
}

// GetConfigByID reads one rate card version.
func (r *PricingRepository) GetConfigByID(ctx context.Context, configID string) (pricing.Config, error) {
	config, err := scanConfig(r.pool.QueryRow(ctx, `SELECT `+configColumns+` FROM pricing_configs WHERE id = $1`, configID))
	if err != nil {
		if isNoRows(err) {
			return pricing.Config{}, pricing.ErrNoActiveConfig
		}

		return pricing.Config{}, fmt.Errorf("select pricing config: %w", err)
	}

	return config, nil
}

func isNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

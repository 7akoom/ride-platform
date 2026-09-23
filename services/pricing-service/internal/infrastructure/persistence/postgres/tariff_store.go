package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/pricing"
	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/tariffs"
)

// maxPastZoneSurges bounds a listing that includes surges that are over.
const maxPastZoneSurges = 100

func (r *PricingRepository) CurrentRateCards(ctx context.Context, filter tariffs.Filter) ([]pricing.Config, error) {
	rows, err := r.pool.Query(
		ctx,
		`SELECT `+configColumns+`
		 FROM (
		     SELECT DISTINCT ON (zone_id, city_id, vehicle_class) *
		     FROM pricing_configs
		     ORDER BY zone_id, city_id, vehicle_class, created_at DESC, id
		 ) pricing_configs
		 WHERE NOT retired
		   AND ($1::uuid IS NULL OR city_id = $1::uuid)
		   AND ($2::uuid IS NULL OR zone_id = $2::uuid)
		 ORDER BY (zone_id IS NOT NULL), (city_id IS NOT NULL), city_id, zone_id,
		          vehicle_class NULLS FIRST`,
		nullableUUID(filter.CityID),
		nullableUUID(filter.ZoneID),
	)
	if err != nil {
		return nil, fmt.Errorf("select rate cards: %w", err)
	}

	cards, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (pricing.Config, error) {
		return scanConfig(row)
	})
	if err != nil {
		return nil, fmt.Errorf("read rate cards: %w", err)
	}

	return cards, nil
}

func (r *PricingRepository) LatestRateCard(ctx context.Context, scope pricing.Scope) (pricing.Config, bool, error) {
	card, err := scanConfig(r.pool.QueryRow(
		ctx,
		`SELECT `+configColumns+`
		 FROM pricing_configs
		 WHERE zone_id IS NOT DISTINCT FROM $1::uuid
		   AND city_id IS NOT DISTINCT FROM $2::uuid
		   AND vehicle_class IS NOT DISTINCT FROM NULLIF($3::text, '')
		 ORDER BY created_at DESC, id
		 LIMIT 1`,
		nullableUUID(scope.ZoneID),
		nullableUUID(scope.CityID),
		scope.VehicleClass,
	))
	if err != nil {
		if isNoRows(err) {
			return pricing.Config{}, false, nil
		}

		return pricing.Config{}, false, fmt.Errorf("select rate card: %w", err)
	}

	return card, true, nil
}

func (r *PricingRepository) InsertRateCard(ctx context.Context, card pricing.Config) (pricing.Config, error) {
	inserted, err := scanConfig(r.pool.QueryRow(
		ctx,
		`INSERT INTO pricing_configs
		    (zone_id, city_id, vehicle_class, currency_code,
		     base_fare, per_km_rate, per_minute_rate, minimum_fare,
		     free_waiting_minutes, waiting_per_minute, cancellation_fee,
		     cancellation_grace_minutes, no_show_fee,
		     max_surge_percent, demand_surge, weather_surge,
		     average_speed_kmh, distance_correction_factor, retired, created_by, created_at)
		 VALUES ($1, $2, NULLIF($3::text, ''), $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
		         $14, $15, $16, $17, $18, $19, $20, clock_timestamp())
		 RETURNING `+configColumns,
		nullableUUID(card.ZoneID),
		nullableUUID(card.CityID),
		card.VehicleClass,
		card.CurrencyCode,
		card.BaseFare,
		card.PerKmRate,
		card.PerMinuteRate,
		card.MinimumFare,
		card.FreeWaitingMinutes,
		card.WaitingPerMinute,
		card.CancellationFee,
		card.CancellationGraceMinutes,
		card.NoShowFee,
		card.MaxSurgePercent,
		card.DemandSurge,
		card.WeatherSurge,
		card.AverageSpeedKmh,
		card.DistanceCorrectionFactor,
		card.Retired,
		nullableUUID(card.CreatedBy),
	))
	if err != nil {
		return pricing.Config{}, fmt.Errorf("insert rate card: %w", err)
	}

	return inserted, nil
}

func (r *PricingRepository) ListSurgeRules(ctx context.Context, filter tariffs.Filter) ([]pricing.SurgeTimeRule, error) {
	rows, err := r.pool.Query(
		ctx,
		`SELECT `+surgeRuleColumns+`
		 FROM surge_time_rules
		 WHERE ($1::uuid IS NULL OR city_id = $1::uuid)
		   AND ($2::uuid IS NULL OR zone_id = $2::uuid)
		 ORDER BY active DESC, start_time, label, id`,
		nullableUUID(filter.CityID),
		nullableUUID(filter.ZoneID),
	)
	if err != nil {
		return nil, fmt.Errorf("select surge rules: %w", err)
	}

	rules, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (pricing.SurgeTimeRule, error) {
		return scanSurgeRule(row)
	})
	if err != nil {
		return nil, fmt.Errorf("read surge rules: %w", err)
	}

	return rules, nil
}

func (r *PricingRepository) CreateSurgeRule(ctx context.Context, rule pricing.SurgeTimeRule) (pricing.SurgeTimeRule, error) {
	created, err := scanSurgeRule(r.pool.QueryRow(
		ctx,
		`INSERT INTO surge_time_rules
		    (label, zone_id, city_id, day_of_week, start_time, end_time, surge_percent, active)
		 VALUES ($1, $2, $3, $4, $5::time, $6::time, $7, TRUE)
		 RETURNING `+surgeRuleColumns,
		rule.Label,
		nullableUUID(rule.ZoneID),
		nullableUUID(rule.CityID),
		rule.DayOfWeek,
		rule.StartTime,
		rule.EndTime,
		rule.SurgePercent,
	))
	if err != nil {
		return pricing.SurgeTimeRule{}, fmt.Errorf("insert surge rule: %w", err)
	}

	return created, nil
}

func (r *PricingRepository) UpdateSurgeRule(ctx context.Context, rule pricing.SurgeTimeRule) (pricing.SurgeTimeRule, error) {
	updated, err := scanSurgeRule(r.pool.QueryRow(
		ctx,
		`UPDATE surge_time_rules
		 SET label = $2, day_of_week = $3, start_time = $4::time, end_time = $5::time,
		     surge_percent = $6, updated_at = CURRENT_TIMESTAMP
		 WHERE id = $1
		 RETURNING `+surgeRuleColumns,
		rule.ID,
		rule.Label,
		rule.DayOfWeek,
		rule.StartTime,
		rule.EndTime,
		rule.SurgePercent,
	))
	if err != nil {
		if isNoRows(err) {
			return pricing.SurgeTimeRule{}, tariffs.ErrSurgeRuleNotFound
		}

		return pricing.SurgeTimeRule{}, fmt.Errorf("update surge rule: %w", err)
	}

	return updated, nil
}

func (r *PricingRepository) SetSurgeRuleActive(ctx context.Context, ruleID string, active bool) (pricing.SurgeTimeRule, error) {
	updated, err := scanSurgeRule(r.pool.QueryRow(
		ctx,
		`UPDATE surge_time_rules
		 SET active = $2, updated_at = CURRENT_TIMESTAMP
		 WHERE id = $1
		 RETURNING `+surgeRuleColumns,
		ruleID,
		active,
	))
	if err != nil {
		if isNoRows(err) {
			return pricing.SurgeTimeRule{}, tariffs.ErrSurgeRuleNotFound
		}

		return pricing.SurgeTimeRule{}, fmt.Errorf("switch surge rule: %w", err)
	}

	return updated, nil
}

func (r *PricingRepository) ListZoneSurges(
	ctx context.Context,
	zoneID string,
	includePast bool,
	now time.Time,
) ([]pricing.ZoneSurge, error) {
	rows, err := r.pool.Query(
		ctx,
		`SELECT `+zoneSurgeColumns+`
		 FROM zone_surges
		 WHERE ($1::uuid IS NULL OR zone_id = $1::uuid)
		   AND ($2 OR (ended_at IS NULL AND ends_at > $3))
		 ORDER BY starts_at DESC, id
		 LIMIT $4`,
		nullableUUID(zoneID),
		includePast,
		now,
		maxPastZoneSurges,
	)
	if err != nil {
		return nil, fmt.Errorf("select zone surges: %w", err)
	}

	surges, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (pricing.ZoneSurge, error) {
		return scanZoneSurge(row)
	})
	if err != nil {
		return nil, fmt.Errorf("read zone surges: %w", err)
	}

	return surges, nil
}

func (r *PricingRepository) CreateZoneSurge(ctx context.Context, surge pricing.ZoneSurge) (pricing.ZoneSurge, error) {
	created, err := scanZoneSurge(r.pool.QueryRow(
		ctx,
		`INSERT INTO zone_surges (zone_id, surge_percent, reason, starts_at, ends_at, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING `+zoneSurgeColumns,
		surge.ZoneID,
		surge.SurgePercent,
		surge.Reason,
		surge.StartsAt,
		surge.EndsAt,
		nullableUUID(surge.CreatedBy),
	))
	if err != nil {
		return pricing.ZoneSurge{}, fmt.Errorf("insert zone surge: %w", err)
	}

	return created, nil
}

func (r *PricingRepository) FindZoneSurge(ctx context.Context, surgeID string) (pricing.ZoneSurge, error) {
	surge, err := scanZoneSurge(r.pool.QueryRow(
		ctx,
		`SELECT `+zoneSurgeColumns+` FROM zone_surges WHERE id = $1`,
		surgeID,
	))
	if err != nil {
		if isNoRows(err) {
			return pricing.ZoneSurge{}, tariffs.ErrZoneSurgeNotFound
		}

		return pricing.ZoneSurge{}, fmt.Errorf("select zone surge: %w", err)
	}

	return surge, nil
}

func (r *PricingRepository) EndZoneSurge(ctx context.Context, surgeID string, now time.Time) (pricing.ZoneSurge, error) {
	ended, err := scanZoneSurge(r.pool.QueryRow(
		ctx,
		`UPDATE zone_surges
		 SET ended_at = $2
		 WHERE id = $1 AND ended_at IS NULL AND ends_at > $2
		 RETURNING `+zoneSurgeColumns,
		surgeID,
		now,
	))
	if err == nil {
		return ended, nil
	}

	if !isNoRows(err) {
		return pricing.ZoneSurge{}, fmt.Errorf("end zone surge: %w", err)
	}

	if _, err := r.FindZoneSurge(ctx, surgeID); err != nil {
		return pricing.ZoneSurge{}, err
	}

	return pricing.ZoneSurge{}, tariffs.ErrZoneSurgeOver
}

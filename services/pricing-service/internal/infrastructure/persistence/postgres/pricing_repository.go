package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/pricing"
)

const uniqueViolationCode = "23505"

const schemaVersion = 1

type PricingRepository struct {
	pool *pgxpool.Pool
}

func NewPricingRepository(pool *pgxpool.Pool) *PricingRepository {
	if pool == nil {
		panic("postgres pool is required")
	}

	return &PricingRepository{pool: pool}
}

// nullableUUID converts an empty string to a real SQL NULL for a UUID
// column — passing "" directly would fail to parse as a UUID.
func nullableUUID(id string) any {
	if id == "" {
		return nil
	}

	return id
}

func (r *PricingRepository) ListActiveSurgeTimeRules(
	ctx context.Context,
) ([]pricing.SurgeTimeRule, error) {
	rows, err := r.pool.Query(
		ctx,
		`SELECT id, label, day_of_week, start_time::text, end_time::text,
		        surge_percent, active
		 FROM surge_time_rules
		 WHERE active = true`,
	)
	if err != nil {
		return nil, fmt.Errorf("select surge time rules: %w", err)
	}
	defer rows.Close()

	var rules []pricing.SurgeTimeRule

	for rows.Next() {
		var rule pricing.SurgeTimeRule
		var dayOfWeek *int16

		if err := rows.Scan(
			&rule.ID,
			&rule.Label,
			&dayOfWeek,
			&rule.StartTime,
			&rule.EndTime,
			&rule.SurgePercent,
			&rule.Active,
		); err != nil {
			return nil, fmt.Errorf("scan surge time rule: %w", err)
		}

		if dayOfWeek != nil {
			day := int(*dayOfWeek)
			rule.DayOfWeek = &day
		}

		rules = append(rules, rule)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate surge time rules: %w", err)
	}

	return rules, nil
}

func (r *PricingRepository) FindCouponByCode(
	ctx context.Context,
	code string,
) (pricing.Coupon, error) {
	row := r.pool.QueryRow(
		ctx,
		`SELECT id, code, discount_type, discount_value,
		        valid_from, valid_until, max_redemptions, redemption_count,
		        per_rider_limit, minimum_fare_amount, active
		 FROM coupons
		 WHERE code = $1`,
		code,
	)

	coupon, err := scanCoupon(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return pricing.Coupon{}, pricing.ErrCouponNotFound
		}

		return pricing.Coupon{}, fmt.Errorf("select coupon: %w", err)
	}

	return coupon, nil
}

func (r *PricingRepository) CreateCoupon(
	ctx context.Context,
	input pricing.CreateCouponInput,
) (pricing.Coupon, error) {
	row := r.pool.QueryRow(
		ctx,
		`INSERT INTO coupons
		    (code, discount_type, discount_value, valid_from, valid_until,
		     max_redemptions, per_rider_limit, minimum_fare_amount)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING id, code, discount_type, discount_value,
		           valid_from, valid_until, max_redemptions, redemption_count,
		           per_rider_limit, minimum_fare_amount, active`,
		input.Code,
		string(input.DiscountType),
		input.DiscountValue,
		input.ValidFrom,
		input.ValidUntil,
		input.MaxRedemptions,
		input.PerRiderLimit,
		input.MinimumFareAmount,
	)

	coupon, err := scanCoupon(row)
	if err != nil {
		var pgErr *pgconn.PgError

		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode {
			return pricing.Coupon{}, pricing.ErrCouponAlreadyExists
		}

		return pricing.Coupon{}, fmt.Errorf("insert coupon: %w", err)
	}

	return coupon, nil
}

func (r *PricingRepository) RiderRedemptionCount(
	ctx context.Context,
	couponID, riderID string,
) (int, error) {
	var count int

	err := r.pool.QueryRow(
		ctx,
		`SELECT COUNT(*)
		 FROM coupon_redemptions
		 WHERE coupon_id = $1 AND rider_id = $2`,
		couponID,
		riderID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count rider coupon redemptions: %w", err)
	}

	return count, nil
}

func (r *PricingRepository) GetRiderCompletedTripCount(
	ctx context.Context,
	riderID string,
) (int, error) {
	var count int

	err := r.pool.QueryRow(
		ctx,
		`SELECT completed_trip_count
		 FROM rider_trip_stats
		 WHERE rider_id = $1`,
		riderID,
	).Scan(&count)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// No row yet means this rider has completed no trips —
			// the expected case for a first-time rider, not an error.
			return 0, nil
		}

		return 0, fmt.Errorf("select rider trip stats: %w", err)
	}

	return count, nil
}

func (r *PricingRepository) FindFareByTripID(
	ctx context.Context,
	tripID string,
) (pricing.Fare, bool, error) {
	row := r.pool.QueryRow(
		ctx,
		`SELECT id, trip_id, rider_id, currency_code,
		        base_fare, distance_km, distance_fare,
		        duration_minutes, duration_fare, subtotal,
		        surge_time_percent, surge_demand_percent, surge_weather_percent,
		        surge_total_percent, surge_amount,
		        applied_discount_type, applied_discount_label, discount_amount,
		        total, COALESCE(zone_id::text, ''), COALESCE(vehicle_class, ''), created_at
		 FROM fares
		 WHERE trip_id = $1`,
		tripID,
	)

	fare, err := scanFare(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return pricing.Fare{}, false, nil
		}

		return pricing.Fare{}, false, fmt.Errorf("select fare by trip id: %w", err)
	}

	return fare, true, nil
}

// PersistFare writes the fare row, bumps the rider's completed-trip
// counter, records the coupon redemption (if any), and emits the outbox
// event — all in one transaction, so a fare can never exist without its
// side effects, or vice versa.
func (r *PricingRepository) PersistFare(
	ctx context.Context,
	input pricing.PersistFareInput,
) (pricing.Fare, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return pricing.Fare{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	b := input.Breakdown

	var discountType, discountLabel *string

	if b.AppliedDiscountType != pricing.DiscountNone {
		typeValue := string(b.AppliedDiscountType)
		discountType = &typeValue
		discountLabel = &b.AppliedDiscountLabel
	}

	row := tx.QueryRow(
		ctx,
		`INSERT INTO fares
		    (trip_id, rider_id, currency_code,
		     base_fare, distance_km, distance_fare,
		     duration_minutes, duration_fare, subtotal,
		     surge_time_percent, surge_demand_percent, surge_weather_percent,
		     surge_total_percent, surge_amount,
		     applied_discount_type, applied_discount_label, discount_amount,
		     total, zone_id, vehicle_class)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9,
		         $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)
		 RETURNING id, trip_id, rider_id, currency_code,
		           base_fare, distance_km, distance_fare,
		           duration_minutes, duration_fare, subtotal,
		           surge_time_percent, surge_demand_percent, surge_weather_percent,
		           surge_total_percent, surge_amount,
		           applied_discount_type, applied_discount_label, discount_amount,
		           total, COALESCE(zone_id::text, ''), COALESCE(vehicle_class, ''), created_at`,
		input.TripID,
		input.RiderID,
		b.CurrencyCode,
		b.BaseFare,
		b.DistanceKm,
		b.DistanceFare,
		b.DurationMinutes,
		b.DurationFare,
		b.Subtotal,
		b.Surge.TimeOfDayPercent,
		b.Surge.DemandPercent,
		b.Surge.WeatherPercent,
		b.Surge.TotalPercent,
		b.SurgeAmount,
		discountType,
		discountLabel,
		b.DiscountAmount,
		b.Total,
		nullableUUID(b.ZoneID),
		b.VehicleClass,
	)

	fare, err := scanFare(row)
	if err != nil {
		var pgErr *pgconn.PgError

		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode {
			// A concurrent call beat us to it. Since fares are
			// immutable per trip, the other one's result is just as
			// valid — roll back and let the caller re-read it.
			return pricing.Fare{}, pricing.ErrTripIDRequired
		}

		return pricing.Fare{}, fmt.Errorf("insert fare: %w", err)
	}

	if _, err := tx.Exec(
		ctx,
		`INSERT INTO rider_trip_stats (rider_id, completed_trip_count, first_completed_at)
		 VALUES ($1, 1, CURRENT_TIMESTAMP)
		 ON CONFLICT (rider_id) DO UPDATE
		 SET completed_trip_count = rider_trip_stats.completed_trip_count + 1,
		     updated_at = CURRENT_TIMESTAMP`,
		input.RiderID,
	); err != nil {
		return pricing.Fare{}, fmt.Errorf("update rider trip stats: %w", err)
	}

	if input.Coupon != nil {
		if _, err := tx.Exec(
			ctx,
			`INSERT INTO coupon_redemptions (coupon_id, rider_id, trip_id, discount_amount)
			 VALUES ($1, $2, $3, $4)`,
			input.Coupon.CouponID,
			input.RiderID,
			input.TripID,
			input.Coupon.DiscountAmount,
		); err != nil {
			return pricing.Fare{}, fmt.Errorf("insert coupon redemption: %w", err)
		}

		if _, err := tx.Exec(
			ctx,
			`UPDATE coupons
			 SET redemption_count = redemption_count + 1,
			     updated_at = CURRENT_TIMESTAMP
			 WHERE id = $1`,
			input.Coupon.CouponID,
		); err != nil {
			return pricing.Fare{}, fmt.Errorf("increment coupon redemption count: %w", err)
		}
	}

	payload, err := json.Marshal(map[string]any{
		"trip_id":       input.TripID,
		"rider_id":      input.RiderID,
		"currency_code": b.CurrencyCode,
		"total":         b.Total,
	})
	if err != nil {
		return pricing.Fare{}, fmt.Errorf("marshal fare.calculated payload: %w", err)
	}

	if _, err := tx.Exec(
		ctx,
		`INSERT INTO outbox_events
		    (aggregate_type, aggregate_id, event_type, schema_version,
		     payload, occurred_at, available_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $6)`,
		"fare",
		fare.ID,
		"fare.calculated",
		schemaVersion,
		payload,
		time.Now().UTC(),
	); err != nil {
		return pricing.Fare{}, fmt.Errorf("insert fare.calculated outbox event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return pricing.Fare{}, fmt.Errorf("commit transaction: %w", err)
	}

	return fare, nil
}

func scanCoupon(row pgx.Row) (pricing.Coupon, error) {
	var coupon pricing.Coupon
	var discountType string
	var maxRedemptions *int32

	err := row.Scan(
		&coupon.ID,
		&coupon.Code,
		&discountType,
		&coupon.DiscountValue,
		&coupon.ValidFrom,
		&coupon.ValidUntil,
		&maxRedemptions,
		&coupon.RedemptionCount,
		&coupon.PerRiderLimit,
		&coupon.MinimumFareAmount,
		&coupon.Active,
	)
	if err != nil {
		return pricing.Coupon{}, err
	}

	coupon.DiscountType = pricing.DiscountType(discountType)

	if maxRedemptions != nil {
		value := int(*maxRedemptions)
		coupon.MaxRedemptions = &value
	}

	return coupon, nil
}

func scanFare(row pgx.Row) (pricing.Fare, error) {
	var fare pricing.Fare
	var b pricing.FareBreakdown
	var discountType, discountLabel *string

	err := row.Scan(
		&fare.ID,
		&fare.TripID,
		&fare.RiderID,
		&b.CurrencyCode,
		&b.BaseFare,
		&b.DistanceKm,
		&b.DistanceFare,
		&b.DurationMinutes,
		&b.DurationFare,
		&b.Subtotal,
		&b.Surge.TimeOfDayPercent,
		&b.Surge.DemandPercent,
		&b.Surge.WeatherPercent,
		&b.Surge.TotalPercent,
		&b.SurgeAmount,
		&discountType,
		&discountLabel,
		&b.DiscountAmount,
		&b.Total,
		&b.ZoneID,
		&b.VehicleClass,
		&fare.CreatedAt,
	)
	if err != nil {
		return pricing.Fare{}, err
	}

	b.Surge.Multiplier = decimal.NewFromInt(1).Add(b.Surge.TotalPercent.Div(decimal.NewFromInt(100)))

	if discountType != nil {
		b.AppliedDiscountType = pricing.DiscountType(*discountType)
	}

	if discountLabel != nil {
		b.AppliedDiscountLabel = *discountLabel
	}

	fare.Breakdown = b

	return fare, nil
}

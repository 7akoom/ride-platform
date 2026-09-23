package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/pricing"
)

// quoteColumns is every quote column, in scanQuote's order.
const quoteColumns = `id, rider_id, zone_id, COALESCE(city_id::text, ''), vehicle_class,
        pickup_latitude, pickup_longitude, dropoff_latitude, dropoff_longitude,
        breakdown, COALESCE(config_id::text, ''), COALESCE(coupon_id::text, ''), discount_amount,
        drivers_available, pickup_eta_minutes, created_at, expires_at,
        COALESCE(claimed_trip_id::text, '')`

// SaveQuotes stores one QuoteTrip call's quotes in one transaction.
func (r *PricingRepository) SaveQuotes(ctx context.Context, quotes []pricing.Quote) ([]pricing.Quote, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	saved := make([]pricing.Quote, 0, len(quotes))

	for _, quote := range quotes {
		breakdown, err := json.Marshal(quote.Breakdown)
		if err != nil {
			return nil, fmt.Errorf("encode quote breakdown: %w", err)
		}

		couponID, discount := "", decimal.Zero
		if quote.Coupon != nil {
			couponID, discount = quote.Coupon.CouponID, quote.Coupon.DiscountAmount
		}

		row := tx.QueryRow(
			ctx,
			`INSERT INTO fare_quotes
			    (rider_id, zone_id, city_id, vehicle_class,
			     pickup_latitude, pickup_longitude, dropoff_latitude, dropoff_longitude,
			     currency_code, total, breakdown, config_id, coupon_id, discount_amount,
			     drivers_available, pickup_eta_minutes, created_at, expires_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
			 RETURNING `+quoteColumns,
			quote.RiderID,
			quote.ZoneID,
			nullableUUID(quote.CityID),
			quote.VehicleClass,
			quote.Pickup.Latitude,
			quote.Pickup.Longitude,
			quote.Dropoff.Latitude,
			quote.Dropoff.Longitude,
			quote.Breakdown.CurrencyCode,
			quote.Breakdown.Total,
			breakdown,
			nullableUUID(quote.ConfigID),
			nullableUUID(couponID),
			discount,
			quote.DriversAvailable,
			quote.PickupETAMinutes,
			quote.CreatedAt,
			quote.ExpiresAt,
		)

		stored, err := scanQuote(row)
		if err != nil {
			return nil, fmt.Errorf("insert quote: %w", err)
		}

		saved = append(saved, stored)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return saved, nil
}

func (r *PricingRepository) FindQuote(ctx context.Context, quoteID string) (pricing.Quote, error) {
	quote, err := scanQuote(r.pool.QueryRow(ctx, `SELECT `+quoteColumns+` FROM fare_quotes WHERE id = $1`, quoteID))
	if err != nil {
		if isNoRows(err) {
			return pricing.Quote{}, pricing.ErrQuoteNotFound
		}

		return pricing.Quote{}, fmt.Errorf("select quote: %w", err)
	}

	return quote, nil
}

// ClaimQuote claims in one conditional UPDATE, so two trips can never both
// take a quote; when it changes nothing, the quote is read to say why.
func (r *PricingRepository) ClaimQuote(
	ctx context.Context,
	quoteID, riderID, tripID string,
	now time.Time,
) (pricing.Quote, error) {
	quote, err := scanQuote(r.pool.QueryRow(
		ctx,
		`UPDATE fare_quotes
		 SET claimed_trip_id = $3, claimed_at = $4
		 WHERE id = $1 AND rider_id = $2 AND claimed_trip_id IS NULL AND expires_at > $4
		 RETURNING `+quoteColumns,
		quoteID, riderID, tripID, now,
	))

	var pgErr *pgconn.PgError

	switch {
	case err == nil:
		return quote, nil
	case errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode:
		// This trip already holds another quote.
		return pricing.Quote{}, pricing.ErrQuoteNotForTrip
	case !isNoRows(err):
		return pricing.Quote{}, fmt.Errorf("claim quote: %w", err)
	}

	current, err := r.FindQuote(ctx, quoteID)
	if err != nil {
		return pricing.Quote{}, err
	}

	switch {
	case current.RiderID != riderID:
		return pricing.Quote{}, pricing.ErrQuoteNotFound
	case current.ClaimedTripID == tripID:
		return current, nil
	case current.ClaimedTripID != "":
		return pricing.Quote{}, pricing.ErrQuoteAlreadyUsed
	default:
		return pricing.Quote{}, pricing.ErrQuoteExpired
	}
}

func (r *PricingRepository) ReleaseQuote(ctx context.Context, quoteID, tripID string) error {
	if _, err := r.pool.Exec(
		ctx,
		`UPDATE fare_quotes
		 SET claimed_trip_id = NULL, claimed_at = NULL
		 WHERE id = $1 AND claimed_trip_id = $2`,
		quoteID, tripID,
	); err != nil {
		return fmt.Errorf("release quote: %w", err)
	}

	return nil
}

func (r *PricingRepository) DeleteUnclaimedQuotes(ctx context.Context, expiredBefore time.Time) (int, error) {
	tag, err := r.pool.Exec(
		ctx,
		`DELETE FROM fare_quotes WHERE claimed_trip_id IS NULL AND expires_at < $1`,
		expiredBefore,
	)
	if err != nil {
		return 0, fmt.Errorf("delete unclaimed quotes: %w", err)
	}

	return int(tag.RowsAffected()), nil
}

func (r *PricingRepository) CountQuotingRiders(
	ctx context.Context,
	zoneID, excludeRiderID string,
	since time.Time,
) (int, error) {
	var count int

	if err := r.pool.QueryRow(
		ctx,
		`SELECT COUNT(DISTINCT rider_id)
		 FROM fare_quotes
		 WHERE zone_id = $1 AND created_at >= $2 AND rider_id <> $3`,
		zoneID, since, excludeRiderID,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("count quoting riders: %w", err)
	}

	return count, nil
}

func (r *PricingRepository) ActiveZoneSurge(
	ctx context.Context,
	zoneID string,
	at time.Time,
) (pricing.ZoneSurge, bool, error) {
	surge, err := scanZoneSurge(r.pool.QueryRow(
		ctx,
		`SELECT `+zoneSurgeColumns+`
		 FROM zone_surges
		 WHERE zone_id = $1 AND ended_at IS NULL AND starts_at <= $2 AND ends_at > $2
		 ORDER BY surge_percent DESC, created_at DESC
		 LIMIT 1`,
		zoneID, at,
	))
	if err != nil {
		if isNoRows(err) {
			return pricing.ZoneSurge{}, false, nil
		}

		return pricing.ZoneSurge{}, false, fmt.Errorf("select zone surge: %w", err)
	}

	return surge, true, nil
}

func scanQuote(row pgx.Row) (pricing.Quote, error) {
	var (
		quote     pricing.Quote
		breakdown []byte
		couponID  string
		discount  decimal.Decimal
	)

	if err := row.Scan(
		&quote.ID,
		&quote.RiderID,
		&quote.ZoneID,
		&quote.CityID,
		&quote.VehicleClass,
		&quote.Pickup.Latitude,
		&quote.Pickup.Longitude,
		&quote.Dropoff.Latitude,
		&quote.Dropoff.Longitude,
		&breakdown,
		&quote.ConfigID,
		&couponID,
		&discount,
		&quote.DriversAvailable,
		&quote.PickupETAMinutes,
		&quote.CreatedAt,
		&quote.ExpiresAt,
		&quote.ClaimedTripID,
	); err != nil {
		return pricing.Quote{}, err
	}

	if err := json.Unmarshal(breakdown, &quote.Breakdown); err != nil {
		return pricing.Quote{}, fmt.Errorf("decode quote breakdown: %w", err)
	}

	if couponID != "" {
		quote.Coupon = &pricing.AppliedCoupon{CouponID: couponID, DiscountAmount: discount}
	}

	return quote, nil
}

// zoneSurgeColumns is every zone surge column, in scanZoneSurge's order.
const zoneSurgeColumns = `id, zone_id, surge_percent, reason, starts_at, ends_at, ended_at,
        COALESCE(created_by::text, ''), created_at`

func scanZoneSurge(row pgx.Row) (pricing.ZoneSurge, error) {
	var surge pricing.ZoneSurge

	if err := row.Scan(
		&surge.ID,
		&surge.ZoneID,
		&surge.SurgePercent,
		&surge.Reason,
		&surge.StartsAt,
		&surge.EndsAt,
		&surge.EndedAt,
		&surge.CreatedBy,
		&surge.CreatedAt,
	); err != nil {
		return pricing.ZoneSurge{}, err
	}

	return surge, nil
}

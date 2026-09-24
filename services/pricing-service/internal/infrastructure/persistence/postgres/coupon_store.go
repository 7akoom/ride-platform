package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/pricing"
	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/promotions"
)

// Redemption statuses: a claimed quote reserves a use of its coupon, a
// completed trip redeems it, a cancelled (or never created) trip releases
// it. coupons.redemption_count counts the reserved and redeemed ones.
const (
	redemptionReserved = "reserved"
	redemptionRedeemed = "redeemed"
)

// couponColumns is every coupon column, in scanCoupon's order.
const couponColumns = `id, code, description, discount_type, discount_value, max_discount_amount,
        valid_from, valid_until, max_redemptions, redemption_count, per_rider_limit,
        minimum_fare_amount, COALESCE(city_id::text, ''), COALESCE(zone_id::text, ''),
        vehicle_classes, new_riders_only, active,
        COALESCE(created_by::text, ''), COALESCE(updated_by::text, ''), created_at, updated_at`

func scanCoupon(row pgx.Row) (pricing.Coupon, error) {
	var (
		coupon         pricing.Coupon
		discountType   string
		maxDiscount    decimal.NullDecimal
		maxRedemptions *int32
	)

	if err := row.Scan(
		&coupon.ID,
		&coupon.Code,
		&coupon.Description,
		&discountType,
		&coupon.DiscountValue,
		&maxDiscount,
		&coupon.ValidFrom,
		&coupon.ValidUntil,
		&maxRedemptions,
		&coupon.RedemptionCount,
		&coupon.PerRiderLimit,
		&coupon.MinimumFareAmount,
		&coupon.CityID,
		&coupon.ZoneID,
		&coupon.VehicleClasses,
		&coupon.NewRidersOnly,
		&coupon.Active,
		&coupon.CreatedBy,
		&coupon.UpdatedBy,
		&coupon.CreatedAt,
		&coupon.UpdatedAt,
	); err != nil {
		return pricing.Coupon{}, err
	}

	coupon.DiscountType = pricing.DiscountType(discountType)

	if maxDiscount.Valid {
		value := maxDiscount.Decimal
		coupon.MaxDiscountAmount = &value
	}

	if maxRedemptions != nil {
		value := int(*maxRedemptions)
		coupon.MaxRedemptions = &value
	}

	return coupon, nil
}

func nullableDecimal(amount *decimal.Decimal) any {
	if amount == nil {
		return nil
	}

	return *amount
}

func (r *PricingRepository) FindCouponByCode(ctx context.Context, code string) (pricing.Coupon, error) {
	coupon, err := scanCoupon(r.pool.QueryRow(ctx, `SELECT `+couponColumns+` FROM coupons WHERE code = $1`, code))
	if err != nil {
		if isNoRows(err) {
			return pricing.Coupon{}, pricing.ErrCouponNotFound
		}

		return pricing.Coupon{}, fmt.Errorf("select coupon: %w", err)
	}

	return coupon, nil
}

// RiderRedemptionCount counts the uses the rider holds: redeemed and
// reserved, never released.
func (r *PricingRepository) RiderRedemptionCount(ctx context.Context, couponID, riderID string) (int, error) {
	var count int

	if err := r.pool.QueryRow(
		ctx,
		`SELECT COUNT(*)
		 FROM coupon_redemptions
		 WHERE coupon_id = $1 AND rider_id = $2 AND status <> 'released'`,
		couponID, riderID,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("count rider coupon redemptions: %w", err)
	}

	return count, nil
}

// holdCoupon takes one use of the coupon for the trip, with the given
// status, if the coupon is still on offer at now and the rider has a use
// left. The coupon row is locked first, so two trips can never both take
// the last use. ErrCouponUnavailable otherwise.
func holdCoupon(
	ctx context.Context,
	tx pgx.Tx,
	couponID, riderID, tripID, quoteID string,
	discount decimal.Decimal,
	status string,
	now time.Time,
) error {
	coupon, err := scanCoupon(tx.QueryRow(ctx, `SELECT `+couponColumns+` FROM coupons WHERE id = $1 FOR UPDATE`, couponID))
	if err != nil {
		if isNoRows(err) {
			return pricing.ErrCouponUnavailable
		}

		return fmt.Errorf("lock coupon: %w", err)
	}

	if !coupon.IsCurrentlyValid(now) {
		return pricing.ErrCouponUnavailable
	}

	var riderUses int

	if err := tx.QueryRow(
		ctx,
		`SELECT COUNT(*)
		 FROM coupon_redemptions
		 WHERE coupon_id = $1 AND rider_id = $2 AND status <> 'released'`,
		couponID, riderID,
	).Scan(&riderUses); err != nil {
		return fmt.Errorf("count rider coupon redemptions: %w", err)
	}

	if riderUses >= coupon.PerRiderLimit {
		return pricing.ErrCouponUnavailable
	}

	if _, err := tx.Exec(
		ctx,
		`INSERT INTO coupon_redemptions (coupon_id, rider_id, trip_id, quote_id, discount_amount, status, redeemed_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		couponID, riderID, tripID, nullableUUID(quoteID), discount, status, now,
	); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode {
			// The trip already holds a coupon.
			return pricing.ErrCouponUnavailable
		}

		return fmt.Errorf("insert coupon redemption: %w", err)
	}

	if _, err := tx.Exec(
		ctx,
		`UPDATE coupons SET redemption_count = redemption_count + 1 WHERE id = $1`,
		couponID,
	); err != nil {
		return fmt.Errorf("count the coupon use: %w", err)
	}

	return nil
}

// redeemCoupon turns the use the trip reserved into a redeemed one, or
// takes a use now when it reserved none.
func redeemCoupon(ctx context.Context, tx pgx.Tx, input pricing.PersistFareInput) error {
	tag, err := tx.Exec(
		ctx,
		`UPDATE coupon_redemptions
		 SET status = 'redeemed', discount_amount = $3
		 WHERE trip_id = $1 AND coupon_id = $2 AND status = 'reserved'`,
		input.TripID, input.Coupon.CouponID, input.Coupon.DiscountAmount,
	)
	if err != nil {
		return fmt.Errorf("redeem the reserved coupon use: %w", err)
	}

	if tag.RowsAffected() == 1 {
		return nil
	}

	return holdCoupon(ctx, tx, input.Coupon.CouponID, input.RiderID, input.TripID, input.QuoteID,
		input.Coupon.DiscountAmount, redemptionRedeemed, time.Now().UTC())
}

// releaseTripCoupon frees the use the trip reserved, if any, in one
// statement.
func releaseTripCoupon(ctx context.Context, q interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}, tripID string,
) error {
	if _, err := q.Exec(
		ctx,
		`WITH released AS (
		     UPDATE coupon_redemptions
		     SET status = 'released', released_at = CURRENT_TIMESTAMP
		     WHERE trip_id = $1 AND status = 'reserved'
		     RETURNING coupon_id
		 )
		 UPDATE coupons
		 SET redemption_count = redemption_count - 1
		 WHERE id IN (SELECT coupon_id FROM released)`,
		tripID,
	); err != nil {
		return fmt.Errorf("release the trip's coupon use: %w", err)
	}

	return nil
}

func (r *PricingRepository) ReleaseTripCoupon(ctx context.Context, tripID string) error {
	return releaseTripCoupon(ctx, r.pool, tripID)
}

// --- staff -------------------------------------------------------------------------

func (r *PricingRepository) CreateCoupon(ctx context.Context, c pricing.Coupon) (pricing.Coupon, error) {
	var maxRedemptions any
	if c.MaxRedemptions != nil {
		maxRedemptions = *c.MaxRedemptions
	}

	created, err := scanCoupon(r.pool.QueryRow(
		ctx,
		`INSERT INTO coupons
		    (code, description, discount_type, discount_value, max_discount_amount,
		     valid_from, valid_until, max_redemptions, per_rider_limit, minimum_fare_amount,
		     city_id, zone_id, vehicle_classes, new_riders_only, active, created_by, updated_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $16)
		 RETURNING `+couponColumns,
		c.Code,
		c.Description,
		string(c.DiscountType),
		c.DiscountValue,
		nullableDecimal(c.MaxDiscountAmount),
		c.ValidFrom,
		c.ValidUntil,
		maxRedemptions,
		c.PerRiderLimit,
		c.MinimumFareAmount,
		nullableUUID(c.CityID),
		nullableUUID(c.ZoneID),
		c.VehicleClasses,
		c.NewRidersOnly,
		c.Active,
		nullableUUID(c.CreatedBy),
	))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode {
			return pricing.Coupon{}, pricing.ErrCouponAlreadyExists
		}

		return pricing.Coupon{}, fmt.Errorf("insert coupon: %w", err)
	}

	return created, nil
}

func (r *PricingRepository) FindCouponDetails(ctx context.Context, code string) (promotions.CouponDetails, error) {
	coupon, err := r.FindCouponByCode(ctx, code)
	if err != nil {
		return promotions.CouponDetails{}, err
	}

	details := promotions.CouponDetails{Coupon: coupon}

	if err := r.pool.QueryRow(
		ctx,
		`SELECT COUNT(*), COALESCE(SUM(discount_amount), 0)
		 FROM coupon_redemptions
		 WHERE coupon_id = $1 AND status = 'redeemed'`,
		coupon.ID,
	).Scan(&details.RedeemedCount, &details.DiscountGiven); err != nil {
		return promotions.CouponDetails{}, fmt.Errorf("sum coupon redemptions: %w", err)
	}

	return details, nil
}

func (r *PricingRepository) ListCoupons(ctx context.Context, filter promotions.CouponFilter) ([]pricing.Coupon, error) {
	rows, err := r.pool.Query(
		ctx,
		`SELECT `+couponColumns+`
		 FROM coupons
		 WHERE ($1 = '' OR left(code, length($1)) = $1)
		   AND CASE $2
		         WHEN 'running' THEN active AND valid_from <= $3 AND valid_until >= $3
		                         AND (max_redemptions IS NULL OR redemption_count < max_redemptions)
		         WHEN 'scheduled' THEN active AND valid_from > $3
		                         AND (max_redemptions IS NULL OR redemption_count < max_redemptions)
		         WHEN 'finished' THEN NOT active OR valid_until < $3
		                         OR (max_redemptions IS NOT NULL AND redemption_count >= max_redemptions)
		         ELSE true
		       END
		 ORDER BY created_at DESC, id
		 OFFSET $4 LIMIT $5`,
		filter.Prefix, filter.State, filter.Now, filter.Offset, filter.Limit,
	)
	if err != nil {
		return nil, fmt.Errorf("select coupons: %w", err)
	}

	coupons, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (pricing.Coupon, error) {
		return scanCoupon(row)
	})
	if err != nil {
		return nil, fmt.Errorf("read coupons: %w", err)
	}

	return coupons, nil
}

func (r *PricingRepository) UpdateCoupon(
	ctx context.Context,
	code string,
	change promotions.CouponChange,
	staffID string,
) (pricing.Coupon, error) {
	var maxRedemptions, perRider any

	if change.MaxRedemptions != nil {
		maxRedemptions = *change.MaxRedemptions
	}

	if change.PerRiderLimit != nil {
		perRider = *change.PerRiderLimit
	}

	updated, err := scanCoupon(r.pool.QueryRow(
		ctx,
		`UPDATE coupons
		 SET description = COALESCE($2, description),
		     valid_until = COALESCE($3, valid_until),
		     max_redemptions = CASE
		         WHEN $4::integer IS NULL THEN max_redemptions
		         WHEN $4::integer = 0 THEN NULL
		         ELSE $4::integer
		     END,
		     per_rider_limit = COALESCE($5::integer, per_rider_limit),
		     minimum_fare_amount = COALESCE($6, minimum_fare_amount),
		     active = COALESCE($7, active),
		     updated_by = $8,
		     updated_at = CURRENT_TIMESTAMP
		 WHERE code = $1
		 RETURNING `+couponColumns,
		code,
		change.Description,
		change.ValidUntil,
		maxRedemptions,
		perRider,
		nullableDecimal(change.MinimumFareAmount),
		change.Active,
		nullableUUID(staffID),
	))
	if err != nil {
		if isNoRows(err) {
			return pricing.Coupon{}, pricing.ErrCouponNotFound
		}

		return pricing.Coupon{}, fmt.Errorf("update coupon: %w", err)
	}

	return updated, nil
}

func (r *PricingRepository) ListRedemptions(
	ctx context.Context,
	couponID string,
	offset, limit int,
) ([]promotions.Redemption, error) {
	rows, err := r.pool.Query(
		ctx,
		`SELECT id, rider_id, COALESCE(trip_id::text, ''), COALESCE(quote_id::text, ''), status,
		        discount_amount, redeemed_at, released_at
		 FROM coupon_redemptions
		 WHERE coupon_id = $1
		 ORDER BY redeemed_at DESC, id
		 OFFSET $2 LIMIT $3`,
		couponID, offset, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("select coupon redemptions: %w", err)
	}

	redemptions, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (promotions.Redemption, error) {
		var redemption promotions.Redemption

		err := row.Scan(
			&redemption.ID,
			&redemption.RiderID,
			&redemption.TripID,
			&redemption.QuoteID,
			&redemption.Status,
			&redemption.DiscountAmount,
			&redemption.CreatedAt,
			&redemption.ReleasedAt,
		)

		return redemption, err
	})
	if err != nil {
		return nil, fmt.Errorf("read coupon redemptions: %w", err)
	}

	return redemptions, nil
}

// --- automatic discounts -------------------------------------------------------------

const promotionSettingsColumns = `first_ride_percent, first_ride_max_amount, loyalty_every, loyalty_percent,
        loyalty_max_amount, COALESCE(updated_by::text, ''), updated_at`

func scanPromotionSettings(row pgx.Row) (pricing.PromotionSettings, error) {
	var (
		settings                 pricing.PromotionSettings
		firstRideMax, loyaltyMax decimal.NullDecimal
	)

	if err := row.Scan(
		&settings.FirstRidePercent,
		&firstRideMax,
		&settings.LoyaltyEvery,
		&settings.LoyaltyPercent,
		&loyaltyMax,
		&settings.UpdatedBy,
		&settings.UpdatedAt,
	); err != nil {
		return pricing.PromotionSettings{}, err
	}

	if firstRideMax.Valid {
		value := firstRideMax.Decimal
		settings.FirstRideMaxAmount = &value
	}

	if loyaltyMax.Valid {
		value := loyaltyMax.Decimal
		settings.LoyaltyMaxAmount = &value
	}

	return settings, nil
}

// GetPromotionSettings returns the one row, or the defaults if it is
// missing.
func (r *PricingRepository) GetPromotionSettings(ctx context.Context) (pricing.PromotionSettings, error) {
	settings, err := scanPromotionSettings(r.pool.QueryRow(ctx, `SELECT `+promotionSettingsColumns+` FROM promotion_settings WHERE id`))
	if err != nil {
		if isNoRows(err) {
			return pricing.DefaultPromotionSettings(), nil
		}

		return pricing.PromotionSettings{}, fmt.Errorf("select promotion settings: %w", err)
	}

	return settings, nil
}

func (r *PricingRepository) SetPromotionSettings(
	ctx context.Context,
	settings pricing.PromotionSettings,
) (pricing.PromotionSettings, error) {
	saved, err := scanPromotionSettings(r.pool.QueryRow(
		ctx,
		`INSERT INTO promotion_settings
		    (id, first_ride_percent, first_ride_max_amount, loyalty_every, loyalty_percent,
		     loyalty_max_amount, updated_by, updated_at)
		 VALUES (true, $1, $2, $3, $4, $5, $6, CURRENT_TIMESTAMP)
		 ON CONFLICT (id) DO UPDATE
		 SET first_ride_percent = EXCLUDED.first_ride_percent,
		     first_ride_max_amount = EXCLUDED.first_ride_max_amount,
		     loyalty_every = EXCLUDED.loyalty_every,
		     loyalty_percent = EXCLUDED.loyalty_percent,
		     loyalty_max_amount = EXCLUDED.loyalty_max_amount,
		     updated_by = EXCLUDED.updated_by,
		     updated_at = EXCLUDED.updated_at
		 RETURNING `+promotionSettingsColumns,
		settings.FirstRidePercent,
		nullableDecimal(settings.FirstRideMaxAmount),
		settings.LoyaltyEvery,
		settings.LoyaltyPercent,
		nullableDecimal(settings.LoyaltyMaxAmount),
		nullableUUID(settings.UpdatedBy),
	))
	if err != nil {
		return pricing.PromotionSettings{}, fmt.Errorf("save promotion settings: %w", err)
	}

	return saved, nil
}

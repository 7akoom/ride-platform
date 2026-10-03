package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/incentives"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

// IncentiveStore keeps incentive campaigns and pays their bonuses into
// drivers' wallets with the same ledger and locking as every other movement.
type IncentiveStore struct {
	wallets *WalletRepository
}

var _ incentives.Store = (*IncentiveStore)(nil)

func NewIncentiveStore(wallets *WalletRepository) *IncentiveStore {
	if wallets == nil {
		panic("wallet repository is required")
	}

	return &IncentiveStore{wallets: wallets}
}

const campaignColumns = `c.id::text, c.name, c.description, c.currency_code, c.starts_at, c.ends_at,
	COALESCE(c.city_id::text, ''), c.zone_ids::text[], c.vehicle_class, c.daily_start_minute, c.daily_end_minute,
	c.time_zone, c.min_acceptance_rate, c.max_cancellation_rate, c.min_rating, c.tiers,
	c.status, c.cancel_reason, c.created_at, c.settled_at,
	(SELECT count(*) FROM incentive_payouts p WHERE p.campaign_id = c.id AND p.status = 'paid'),
	(SELECT COALESCE(sum(p.amount), 0) FROM incentive_payouts p WHERE p.campaign_id = c.id AND p.status = 'paid')`

type tierJSON struct {
	Trips  int    `json:"trips"`
	Amount string `json:"amount"`
}

func scanCampaign(row pgx.Row) (incentives.Campaign, error) {
	var (
		c          incentives.Campaign
		tiers      []byte
		settledAt  *time.Time
		start, end int16
	)

	if err := row.Scan(
		&c.ID, &c.Name, &c.Description, &c.CurrencyCode, &c.StartsAt, &c.EndsAt,
		&c.CityID, &c.ZoneIDs, &c.VehicleClass, &start, &end,
		&c.TimeZone, &c.MinAcceptanceRate, &c.MaxCancellationRate, &c.MinRating, &tiers,
		&c.Status, &c.CancelReason, &c.CreatedAt, &settledAt,
		&c.PaidDrivers, &c.PaidTotal,
	); err != nil {
		return incentives.Campaign{}, err
	}

	c.DailyStart, c.DailyEnd = int(start), int(end)

	if settledAt != nil {
		c.SettledAt = *settledAt
	}

	var parsed []tierJSON
	if err := json.Unmarshal(tiers, &parsed); err != nil {
		return incentives.Campaign{}, fmt.Errorf("read tiers: %w", err)
	}

	for _, t := range parsed {
		amount, err := decimal.NewFromString(t.Amount)
		if err != nil {
			return incentives.Campaign{}, fmt.Errorf("read tier amount: %w", err)
		}

		c.Tiers = append(c.Tiers, incentives.Tier{Trips: t.Trips, Amount: amount})
	}

	return c, nil
}

func (s *IncentiveStore) Config(ctx context.Context) (wallet.Config, error) {
	return s.wallets.GetActiveConfig(ctx)
}

func (s *IncentiveStore) Create(ctx context.Context, c incentives.Campaign, createdBy, key string) (incentives.Campaign, error) {
	tiers := make([]tierJSON, 0, len(c.Tiers))
	for _, t := range c.Tiers {
		tiers = append(tiers, tierJSON{Trips: t.Trips, Amount: t.Amount.String()})
	}

	body, err := json.Marshal(tiers)
	if err != nil {
		return incentives.Campaign{}, fmt.Errorf("marshal tiers: %w", err)
	}

	zones := c.ZoneIDs
	if zones == nil {
		zones = []string{}
	}

	var id string

	err = s.wallets.pool.QueryRow(
		ctx,
		`INSERT INTO incentive_campaigns
		    (name, description, currency_code, starts_at, ends_at, city_id, zone_ids, vehicle_class,
		     daily_start_minute, daily_end_minute, time_zone,
		     min_acceptance_rate, max_cancellation_rate, min_rating, tiers, created_by, idempotency_key)
		 VALUES ($1, $2, $3, $4, $5, NULLIF($6, '')::uuid, $7::uuid[], $8, $9, $10, $11, $12, $13, $14, $15,
		         NULLIF($16, '')::uuid, NULLIF($17, ''))
		 RETURNING id::text`,
		c.Name, c.Description, c.CurrencyCode, c.StartsAt, c.EndsAt, c.CityID, zones, c.VehicleClass,
		c.DailyStart, c.DailyEnd, c.TimeZone,
		c.MinAcceptanceRate, c.MaxCancellationRate, c.MinRating, body, createdBy, key,
	).Scan(&id)
	if err != nil {
		if isUniqueViolation(err, "incentive_campaigns_idempotency_key_unique") {
			earlier, found, findErr := s.FindByKey(ctx, key)
			if findErr == nil && found {
				return earlier, nil
			}
		}

		return incentives.Campaign{}, fmt.Errorf("insert campaign: %w", err)
	}

	return s.Get(ctx, id)
}

func (s *IncentiveStore) FindByKey(ctx context.Context, key string) (incentives.Campaign, bool, error) {
	c, err := scanCampaign(s.wallets.pool.QueryRow(ctx,
		`SELECT `+campaignColumns+` FROM incentive_campaigns c WHERE c.idempotency_key = $1`, key))

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return incentives.Campaign{}, false, nil
	case err != nil:
		return incentives.Campaign{}, false, fmt.Errorf("select campaign: %w", err)
	}

	return c, true, nil
}

func (s *IncentiveStore) Get(ctx context.Context, id string) (incentives.Campaign, error) {
	c, err := scanCampaign(s.wallets.pool.QueryRow(ctx,
		`SELECT `+campaignColumns+` FROM incentive_campaigns c WHERE c.id = $1`, id))

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return incentives.Campaign{}, incentives.ErrCampaignNotFound
	case err != nil:
		return incentives.Campaign{}, fmt.Errorf("select campaign: %w", err)
	}

	return c, nil
}

// stateCondition is the SQL for a state, at the time $1.
var stateCondition = map[string]string{
	string(incentives.StateScheduled): `c.status = 'active' AND c.starts_at > $1`,
	string(incentives.StateRunning):   `c.status = 'active' AND c.starts_at <= $1 AND c.ends_at > $1`,
	string(incentives.StateSettling):  `(c.status = 'settling' OR (c.status = 'active' AND c.ends_at <= $1))`,
	string(incentives.StateSettled):   `c.status = 'settled'`,
	string(incentives.StateCancelled): `c.status = 'cancelled'`,
}

func (s *IncentiveStore) List(ctx context.Context, query incentives.CampaignsQuery) ([]incentives.Campaign, error) {
	sql := `SELECT ` + campaignColumns + ` FROM incentive_campaigns c WHERE $1::timestamptz IS NOT NULL`
	args := []any{query.Now}

	if condition, ok := stateCondition[query.Status]; ok {
		sql += ` AND ` + condition
	}

	if query.AfterID != "" {
		args = append(args, query.AfterID)
		sql += fmt.Sprintf(` AND (c.created_at, c.id) < (SELECT created_at, id FROM incentive_campaigns WHERE id = $%d)`, len(args))
	}

	args = append(args, query.Limit)
	sql += fmt.Sprintf(` ORDER BY c.created_at DESC, c.id DESC LIMIT $%d`, len(args))

	rows, err := s.wallets.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("list campaigns: %w", err)
	}

	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (incentives.Campaign, error) {
		return scanCampaign(row)
	})
}

func (s *IncentiveStore) Cancel(ctx context.Context, id, reason string, now time.Time) (incentives.Campaign, error) {
	tag, err := s.wallets.pool.Exec(ctx,
		`UPDATE incentive_campaigns
		 SET status = 'cancelled', cancel_reason = $2, updated_at = CURRENT_TIMESTAMP
		 WHERE id = $1 AND status = 'active' AND ends_at > $3`,
		id, reason, now)
	if err != nil {
		return incentives.Campaign{}, fmt.Errorf("cancel campaign: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return incentives.Campaign{}, incentives.ErrNotCancellable
	}

	return s.Get(ctx, id)
}

func (s *IncentiveStore) ClaimDue(ctx context.Context, endedBefore time.Time, lease time.Duration) (incentives.Campaign, bool, error) {
	var id string

	err := s.wallets.pool.QueryRow(ctx,
		`UPDATE incentive_campaigns
		 SET status = 'settling', settle_lease_until = now() + $2::interval, updated_at = CURRENT_TIMESTAMP
		 WHERE id = (
		     SELECT id FROM incentive_campaigns
		     WHERE status IN ('active', 'settling') AND ends_at <= $1
		       AND (settle_lease_until IS NULL OR settle_lease_until < now())
		     ORDER BY ends_at, id
		     LIMIT 1
		     FOR UPDATE SKIP LOCKED)
		 RETURNING id::text`,
		endedBefore, fmt.Sprintf("%d seconds", int(lease.Seconds())),
	).Scan(&id)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return incentives.Campaign{}, false, nil
	case err != nil:
		return incentives.Campaign{}, false, fmt.Errorf("claim a due campaign: %w", err)
	}

	c, err := s.Get(ctx, id)
	if err != nil {
		return incentives.Campaign{}, false, err
	}

	return c, true, nil
}

const incentivePayoutColumns = `campaign_id::text, driver_id::text, completed_trips, acceptance_rate, cancellation_rate,
	rating, tier_trips, amount, status, unmet, created_at`

func scanIncentivePayout(row pgx.Row) (incentives.Payout, error) {
	var p incentives.Payout

	err := row.Scan(&p.CampaignID, &p.DriverID, &p.CompletedTrips, &p.AcceptanceRate, &p.CancellationRate,
		&p.Rating, &p.TierTrips, &p.Amount, &p.Status, &p.Unmet, &p.CreatedAt)

	return p, err
}

func (s *IncentiveStore) Settle(ctx context.Context, c incentives.Campaign, payout incentives.Payout) (incentives.Payout, error) {
	config, err := s.wallets.GetActiveConfig(ctx)
	if err != nil {
		return incentives.Payout{}, err
	}

	tx, err := s.wallets.pool.Begin(ctx)
	if err != nil {
		return incentives.Payout{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	earlier, err := scanIncentivePayout(tx.QueryRow(ctx,
		`SELECT `+incentivePayoutColumns+` FROM incentive_payouts WHERE campaign_id = $1 AND driver_id = $2`,
		c.ID, payout.DriverID))

	switch {
	case err == nil:
		return earlier, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return incentives.Payout{}, fmt.Errorf("look for an earlier payout: %w", err)
	}

	var transactionID *string

	if payout.Status == incentives.PayoutPaid {
		driver, err := ensureWalletTx(ctx, tx, wallet.OwnerDriver, payout.DriverID, c.CurrencyCode)
		if err != nil {
			return incentives.Payout{}, err
		}

		floor := config.SuspensionFloor()

		_, row, err := applyMovementTx(ctx, tx, driver, wallet.MovementInput{
			OwnerType: wallet.OwnerDriver, OwnerID: payout.DriverID, Type: wallet.TxIncentive,
			Amount:          payout.Amount,
			IdempotencyKey:  "incentive:" + c.ID + ":" + payout.DriverID,
			Description:     "Incentive: " + c.Name,
			SuspensionFloor: &floor,
		})
		if err != nil {
			return incentives.Payout{}, err
		}

		transactionID = &row.ID
	}

	unmet := payout.Unmet
	if unmet == nil {
		unmet = []string{}
	}

	saved, err := scanIncentivePayout(tx.QueryRow(ctx,
		`INSERT INTO incentive_payouts
		    (campaign_id, driver_id, completed_trips, acceptance_rate, cancellation_rate, rating,
		     tier_trips, amount, status, unmet, transaction_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		 RETURNING `+incentivePayoutColumns,
		c.ID, payout.DriverID, payout.CompletedTrips, payout.AcceptanceRate, payout.CancellationRate, payout.Rating,
		payout.TierTrips, payout.Amount, payout.Status, unmet, transactionID,
	))
	if err != nil {
		return incentives.Payout{}, fmt.Errorf("insert payout: %w", err)
	}

	if saved.Status == incentives.PayoutPaid {
		body, err := json.Marshal(map[string]any{
			"campaign_id":   c.ID,
			"campaign_name": c.Name,
			"driver_id":     saved.DriverID,
			"amount":        saved.Amount.String(),
			"currency_code": c.CurrencyCode,
			"trips":         saved.CompletedTrips,
		})
		if err != nil {
			return incentives.Payout{}, fmt.Errorf("marshal wallet.incentive_paid payload: %w", err)
		}

		if _, err := tx.Exec(ctx,
			`INSERT INTO outbox_events
			    (aggregate_type, aggregate_id, event_type, schema_version, payload, occurred_at, available_at)
			 VALUES ('incentive', $1, 'wallet.incentive_paid', $2, $3, $4, $4)`,
			c.ID, schemaVersion, body, time.Now().UTC(),
		); err != nil {
			return incentives.Payout{}, fmt.Errorf("insert wallet.incentive_paid outbox event: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return incentives.Payout{}, fmt.Errorf("commit transaction: %w", err)
	}

	return saved, nil
}

func (s *IncentiveStore) MarkSettled(ctx context.Context, id string, at time.Time) error {
	if _, err := s.wallets.pool.Exec(ctx,
		`UPDATE incentive_campaigns
		 SET status = 'settled', settled_at = $2, settle_lease_until = NULL, updated_at = CURRENT_TIMESTAMP
		 WHERE id = $1 AND status = 'settling'`,
		id, at); err != nil {
		return fmt.Errorf("mark campaign settled: %w", err)
	}

	return nil
}

func (s *IncentiveStore) Payouts(ctx context.Context, campaignID, afterDriverID string, limit int) ([]incentives.Payout, error) {
	after := afterDriverID
	if after == "" {
		after = "00000000-0000-0000-0000-000000000000"
	}

	rows, err := s.wallets.pool.Query(ctx,
		`SELECT `+incentivePayoutColumns+` FROM incentive_payouts
		 WHERE campaign_id = $1 AND driver_id > $2::uuid
		 ORDER BY driver_id LIMIT $3`,
		campaignID, after, limit)
	if err != nil {
		return nil, fmt.Errorf("list payouts: %w", err)
	}

	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (incentives.Payout, error) {
		return scanIncentivePayout(row)
	})
}

func (s *IncentiveStore) DriverPayouts(ctx context.Context, driverID string, campaignIDs []string) (map[string]incentives.Payout, error) {
	out := map[string]incentives.Payout{}

	if len(campaignIDs) == 0 {
		return out, nil
	}

	rows, err := s.wallets.pool.Query(ctx,
		`SELECT `+incentivePayoutColumns+` FROM incentive_payouts WHERE driver_id = $1 AND campaign_id = ANY($2::uuid[])`,
		driverID, campaignIDs)
	if err != nil {
		return nil, fmt.Errorf("select the driver's payouts: %w", err)
	}

	payouts, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (incentives.Payout, error) {
		return scanIncentivePayout(row)
	})
	if err != nil {
		return nil, err
	}

	for _, p := range payouts {
		out[p.CampaignID] = p
	}

	return out, nil
}

func (s *IncentiveStore) Visible(ctx context.Context, endedAfter time.Time) ([]incentives.Campaign, error) {
	rows, err := s.wallets.pool.Query(ctx,
		`SELECT `+campaignColumns+` FROM incentive_campaigns c
		 WHERE c.status <> 'cancelled' AND c.ends_at > $1
		 ORDER BY c.starts_at, c.id`,
		endedAfter)
	if err != nil {
		return nil, fmt.Errorf("list visible campaigns: %w", err)
	}

	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (incentives.Campaign, error) {
		return scanCampaign(row)
	})
}

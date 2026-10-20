package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/dues"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

var _ dues.Store = (*WalletRepository)(nil)

// DuesPolicy is the active config's. See dues.Store.
func (r *WalletRepository) DuesPolicy(ctx context.Context) (dues.Policy, error) {
	config, err := r.GetActiveConfig(ctx)
	if err != nil {
		return dues.Policy{}, err
	}

	return dues.Policy{BlockTrips: config.BlockTripsWithDues, GraceDays: config.DuesGraceDays}, nil
}

// SetDuesPolicy changes the active config in place.
func (r *WalletRepository) SetDuesPolicy(ctx context.Context, policy dues.Policy) (dues.Policy, error) {
	var out dues.Policy

	err := r.pool.QueryRow(
		ctx,
		`UPDATE wallet_configs
		 SET block_trips_with_dues = $1, dues_grace_days = $2
		 WHERE id = (SELECT id FROM wallet_configs ORDER BY created_at DESC LIMIT 1)
		 RETURNING block_trips_with_dues, dues_grace_days`,
		policy.BlockTrips,
		policy.GraceDays,
	).Scan(&out.BlockTrips, &out.GraceDays)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return dues.Policy{}, wallet.ErrNoActiveConfig
		}

		return dues.Policy{}, fmt.Errorf("update the dues policy: %w", err)
	}

	return out, nil
}

// ListOverdueRiders groups the unpaid fees by rider (trip_settlements_rider_dues_idx).
func (r *WalletRepository) ListOverdueRiders(ctx context.Context, oldestBefore time.Time, limit int) ([]dues.OverdueRider, error) {
	rows, err := r.pool.Query(
		ctx,
		`SELECT rider_id::text, currency_code, SUM(due_amount - due_paid), MIN(created_at)
		 FROM trip_settlements
		 WHERE due_amount > due_paid
		 GROUP BY rider_id, currency_code
		 HAVING MIN(created_at) < $1
		 ORDER BY MIN(created_at), rider_id
		 LIMIT $2`,
		oldestBefore,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("select overdue riders: %w", err)
	}

	riders, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (dues.OverdueRider, error) {
		var rider dues.OverdueRider

		err := row.Scan(&rider.RiderID, &rider.CurrencyCode, &rider.Outstanding, &rider.OldestAt)

		return rider, err
	})
	if err != nil {
		return nil, fmt.Errorf("read overdue riders: %w", err)
	}

	return riders, nil
}

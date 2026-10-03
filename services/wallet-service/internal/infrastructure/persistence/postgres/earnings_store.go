package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/earnings"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

// EarningsStore sums drivers' settlements and wallet movements by local day.
type EarningsStore struct {
	wallets *WalletRepository
}

var _ earnings.Store = (*EarningsStore)(nil)

func NewEarningsStore(wallets *WalletRepository) *EarningsStore {
	if wallets == nil {
		panic("wallet repository is required")
	}

	return &EarningsStore{wallets: wallets}
}

func (s *EarningsStore) Config(ctx context.Context) (wallet.Config, error) {
	return s.wallets.GetActiveConfig(ctx)
}

func (s *EarningsStore) Days(ctx context.Context, driverID string, from, to time.Time, timeZone string) ([]earnings.Day, error) {
	rows, err := s.wallets.pool.Query(
		ctx,
		`WITH settled AS (
		    SELECT (created_at AT TIME ZONE $4)::date AS day,
		           count(*) FILTER (WHERE kind = 'trip') AS trips,
		           COALESCE(sum(fare_amount) FILTER (WHERE kind = 'trip'), 0) AS fares,
		           COALESCE(sum(commission_amount), 0) AS commission,
		           COALESCE(sum(driver_earning) FILTER (WHERE kind = 'trip'), 0) AS trip_earnings,
		           COALESCE(sum(driver_earning) FILTER (WHERE kind <> 'trip'), 0) AS fees,
		           COALESCE(sum(cash_amount) FILTER (WHERE kind = 'trip'), 0) AS cash_collected,
		           COALESCE(sum(wallet_amount) FILTER (WHERE kind = 'trip'), 0) AS wallet_paid
		    FROM trip_settlements
		    WHERE driver_id = $1 AND created_at >= $2 AND created_at < $3
		    GROUP BY 1
		 ),
		 moved AS (
		    SELECT (tx.created_at AT TIME ZONE $4)::date AS day,
		           COALESCE(sum(tx.amount) FILTER (WHERE tx.type = 'tip'), 0) AS tips,
		           COALESCE(sum(tx.amount) FILTER (WHERE tx.type = 'incentive'), 0) AS incentives,
		           COALESCE(sum(tx.amount) FILTER (WHERE tx.type = 'refund'), 0) AS refunds,
		           COALESCE(sum(tx.amount) FILTER (WHERE tx.type = 'adjustment'), 0) AS adjustments
		    FROM wallet_transactions AS tx
		    JOIN wallets AS w ON w.id = tx.wallet_id
		    WHERE w.owner_type = 'driver' AND w.owner_id = $1
		      AND tx.created_at >= $2 AND tx.created_at < $3
		      AND tx.type IN ('tip', 'incentive', 'refund', 'adjustment')
		    GROUP BY 1
		 )
		 SELECT to_char(COALESCE(s.day, m.day), 'YYYY-MM-DD'),
		        COALESCE(s.trips, 0), COALESCE(s.fares, 0), COALESCE(s.commission, 0),
		        COALESCE(s.trip_earnings, 0), COALESCE(s.fees, 0),
		        COALESCE(m.tips, 0), COALESCE(m.incentives, 0), COALESCE(m.refunds, 0), COALESCE(m.adjustments, 0),
		        COALESCE(s.cash_collected, 0), COALESCE(s.wallet_paid, 0)
		 FROM settled AS s
		 FULL JOIN moved AS m ON m.day = s.day
		 ORDER BY 1`,
		driverID, from, to, timeZone,
	)
	if err != nil {
		return nil, fmt.Errorf("sum earnings: %w", err)
	}

	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (earnings.Day, error) {
		var d earnings.Day

		err := row.Scan(&d.Date, &d.Totals.Trips, &d.Totals.Fares, &d.Totals.Commission,
			&d.Totals.TripEarnings, &d.Totals.Fees,
			&d.Totals.Tips, &d.Totals.Incentives, &d.Totals.Refunds, &d.Totals.Adjustments,
			&d.Totals.CashCollected, &d.Totals.WalletPaid)

		return d, err
	})
}

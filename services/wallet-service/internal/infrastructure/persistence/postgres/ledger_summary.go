package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

// LedgerSummaries reads the totals the business reports need.
type LedgerSummaries struct {
	pool *pgxpool.Pool
}

func NewLedgerSummaries(pool *pgxpool.Pool) *LedgerSummaries {
	return &LedgerSummaries{pool: pool}
}

// Summarize totals movements in [start, end), days cut on timeZone (IANA).
func (s *LedgerSummaries) Summarize(ctx context.Context, start, end time.Time, timeZone string) (wallet.LedgerSummary, error) {
	var out wallet.LedgerSummary

	rows, err := s.pool.Query(ctx, `
		SELECT (t.created_at AT TIME ZONE $3)::date, w.owner_type, t.type, count(*),
		       COALESCE(sum(t.amount) FILTER (WHERE t.amount > 0), 0),
		       COALESCE(-sum(t.amount) FILTER (WHERE t.amount < 0), 0)
		FROM wallet_transactions t
		JOIN wallets w ON w.id = t.wallet_id
		WHERE t.created_at >= $1 AND t.created_at < $2
		GROUP BY 1, 2, 3
		ORDER BY 1, 2, 3`,
		start, end, timeZone)
	if err != nil {
		return out, fmt.Errorf("sum ledger: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var total wallet.LedgerTotal
		if err := rows.Scan(&total.Date, &total.OwnerType, &total.Type, &total.Entries, &total.Credited, &total.Debited); err != nil {
			return out, fmt.Errorf("scan ledger total: %w", err)
		}

		out.Totals = append(out.Totals, total)
	}

	if err := rows.Err(); err != nil {
		return out, fmt.Errorf("read ledger totals: %w", err)
	}

	err = s.pool.QueryRow(ctx, `
		SELECT COALESCE(min(currency_code), ''),
		       COALESCE(sum(balance) FILTER (WHERE owner_type = 'rider'), 0),
		       COALESCE(sum(balance) FILTER (WHERE owner_type = 'driver' AND balance > 0), 0),
		       COALESCE(-sum(balance) FILTER (WHERE owner_type = 'driver' AND balance < 0), 0),
		       count(*) FILTER (WHERE owner_type = 'driver' AND blocked),
		       (SELECT COALESCE(sum(due_amount - due_paid), 0) FROM trip_settlements WHERE due_amount > due_paid)
		FROM wallets`).
		Scan(&out.CurrencyCode, &out.RiderBalances, &out.DriverCredit, &out.DriverDebt, &out.SuspendedDrivers, &out.RiderDues)
	if err != nil {
		return out, fmt.Errorf("sum balances: %w", err)
	}

	return out, nil
}

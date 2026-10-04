package postgres

import (
	"context"
	"fmt"

	dataexportv1 "github.com/7akoom/ride-platform/gen/go/ride/dataexport/v1"
	"github.com/jackc/pgx/v5/pgxpool"
)

// personalDataQuery returns one JSON array (NULL when there is nothing).
// owner picks the id it takes as $1: identity, rider or driver; it is skipped
// when the person has no such profile.
type personalDataQuery struct {
	name  string
	owner string
	sql   string
}

// PersonalDataExporter reads what this service keeps about one person, as
// readable JSON. Internal ids of other services (media ids, hashes, keys) are
// left out.
type PersonalDataExporter struct {
	pool *pgxpool.Pool
}

func NewPersonalDataExporter(pool *pgxpool.Pool) *PersonalDataExporter {
	if pool == nil {
		panic("database pool is required")
	}

	return &PersonalDataExporter{pool: pool}
}

func (e *PersonalDataExporter) Export(ctx context.Context, identityID, riderID, driverID string) ([]*dataexportv1.DataSection, error) {
	ids := map[string]string{"identity": identityID, "rider": riderID, "driver": driverID}

	var out []*dataexportv1.DataSection

	for _, q := range personalDataQueries {
		id := ids[q.owner]
		if id == "" {
			continue
		}

		var content string

		if err := e.pool.QueryRow(ctx,
			`SELECT jsonb_pretty(COALESCE((`+q.sql+`), '[]'::jsonb))`, id).Scan(&content); err != nil {
			return nil, fmt.Errorf("export %s: %w", q.name, err)
		}

		out = append(out, &dataexportv1.DataSection{Name: q.name, Content: []byte(content)})
	}

	return out, nil
}

const exportTransactionsQuery = `SELECT jsonb_agg(to_jsonb(x) ORDER BY x.created_at, x.seq) FROM (
		SELECT t.id, t.type, t.amount, t.balance_after, t.trip_id, t.transfer_id, t.description, t.created_at, t.seq,
		       w.currency_code
		FROM wallet_transactions t JOIN wallets w ON w.id = t.wallet_id
		WHERE w.owner_type = %s AND w.owner_id = $1::uuid) x`

var personalDataQueries = []personalDataQuery{
	{"wallet/rider_wallet.json", "rider", `SELECT jsonb_agg(to_jsonb(x)) FROM (
		SELECT currency_code, balance, blocked, created_at, updated_at FROM wallets
		WHERE owner_type = 'rider' AND owner_id = $1::uuid) x`},
	{"wallet/rider_transactions.json", "rider", fmt.Sprintf(exportTransactionsQuery, "'rider'")},
	{"wallet/transfers.json", "rider", `SELECT jsonb_agg(to_jsonb(x) ORDER BY x.created_at) FROM (
		SELECT id, CASE WHEN sender_rider_id = $1::uuid THEN 'sent' ELSE 'received' END AS direction,
		       CASE WHEN sender_rider_id = $1::uuid THEN recipient_phone ELSE sender_phone END AS other_phone,
		       currency_code, amount, note, created_at
		FROM wallet_transfers WHERE sender_rider_id = $1::uuid OR recipient_rider_id = $1::uuid) x`},
	{"wallet/money_requests.json", "rider", `SELECT jsonb_agg(to_jsonb(x) ORDER BY x.created_at) FROM (
		SELECT id, CASE WHEN requester_rider_id = $1::uuid THEN 'asked' ELSE 'asked_of_me' END AS direction,
		       is_open, currency_code, amount, note, status, expires_at, closed_at, created_at
		FROM money_requests WHERE requester_rider_id = $1::uuid OR payer_rider_id = $1::uuid) x`},
	{"wallet/driver_wallet.json", "driver", `SELECT jsonb_agg(to_jsonb(x)) FROM (
		SELECT currency_code, balance, blocked, created_at, updated_at FROM wallets
		WHERE owner_type = 'driver' AND owner_id = $1::uuid) x`},
	{"wallet/driver_transactions.json", "driver", fmt.Sprintf(exportTransactionsQuery, "'driver'")},
	{"wallet/payouts.json", "driver", `SELECT jsonb_agg(to_jsonb(x) ORDER BY x.created_at) FROM (
		SELECT id, currency_code, amount, destination, status, approved_at, paid_at, paid_reference,
		       rejected_at, reject_reason, created_at
		FROM payout_requests WHERE driver_id = $1::uuid) x`},
}

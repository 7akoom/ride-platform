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

var personalDataQueries = []personalDataQuery{
	{"notifications/rider_inbox.json", "rider", `SELECT jsonb_agg(to_jsonb(x) ORDER BY x.created_at) FROM (
		SELECT event_key, title, body, locale, read_at, created_at FROM notifications
		WHERE recipient_type = 'rider' AND recipient_id = $1::uuid) x`},
	{"notifications/rider_devices.json", "rider", `SELECT jsonb_agg(to_jsonb(x) ORDER BY x.created_at) FROM (
		SELECT platform, locale, last_seen_at, created_at FROM devices
		WHERE recipient_type = 'rider' AND recipient_id = $1::uuid) x`},
	{"notifications/driver_inbox.json", "driver", `SELECT jsonb_agg(to_jsonb(x) ORDER BY x.created_at) FROM (
		SELECT event_key, title, body, locale, read_at, created_at FROM notifications
		WHERE recipient_type = 'driver' AND recipient_id = $1::uuid) x`},
	{"notifications/driver_devices.json", "driver", `SELECT jsonb_agg(to_jsonb(x) ORDER BY x.created_at) FROM (
		SELECT platform, locale, last_seen_at, created_at FROM devices
		WHERE recipient_type = 'driver' AND recipient_id = $1::uuid) x`},
}

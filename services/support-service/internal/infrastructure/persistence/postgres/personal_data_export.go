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
	{"support/tickets.json", "identity", `SELECT jsonb_agg(to_jsonb(x) ORDER BY x.created_at) FROM (
		SELECT t.number, t.audience, t.category_key, t.subject, t.status, t.trip_id, t.created_at, t.resolved_at,
		       t.closed_at, t.rating, t.rating_comment, t.rated_at,
		       (SELECT COALESCE(jsonb_agg(jsonb_build_object(
		                 'from', CASE WHEN m.author_identity_id = $1::uuid THEN 'you' ELSE m.author END,
		                 'body', m.body, 'created_at', m.created_at) ORDER BY m.created_at), '[]'::jsonb)
		        FROM support_messages m WHERE m.ticket_id = t.id AND NOT m.internal) AS messages
		FROM support_tickets t WHERE t.requester_identity_id = $1::uuid) x`},
}

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
	{"driver/profile.json", "driver", `SELECT jsonb_agg(to_jsonb(x)) FROM (
		SELECT id, display_name, status, availability_status, vehicle_make, vehicle_model, vehicle_color,
		       vehicle_plate_number, vehicle_year, vehicle_class, rating_average, rating_count, rejection_reason,
		       created_at, updated_at
		FROM drivers WHERE id = $1::uuid) x`},
	{"driver/details.json", "driver", `SELECT jsonb_agg(to_jsonb(x)) FROM (
		SELECT gender, date_of_birth, nationality, updated_at FROM driver_details WHERE driver_id = $1::uuid) x`},
	{"driver/vehicles.json", "driver", `SELECT jsonb_agg(to_jsonb(x) ORDER BY x.created_at) FROM (
		SELECT make, model, color, plate_number, year, vehicle_class, status, active, rejection_reason,
		       reviewed_at, retired_at, created_at
		FROM vehicles WHERE driver_id = $1::uuid) x`},
	{"driver/documents.json", "driver", `SELECT jsonb_agg(to_jsonb(x) ORDER BY x.created_at) FROM (
		SELECT type_code, document_number, expires_on, status, rejection_reason, reviewed_at, superseded_at, created_at
		FROM driver_documents WHERE driver_id = $1::uuid) x`},
	{"driver/name_changes.json", "driver", `SELECT jsonb_agg(to_jsonb(x) ORDER BY x.created_at) FROM (
		SELECT current_name, requested_name, reason, status, rejection_reason, created_at, decided_at
		FROM driver_name_changes WHERE driver_id = $1::uuid) x`},
}

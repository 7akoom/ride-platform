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

const exportTripColumns = `id, status, requested_at, accepted_at, arrived_at, started_at, completed_at, cancelled_at,
	cancelled_by, cancellation_reason, rider_no_show, vehicle_class, payment_method,
	pickup_latitude, pickup_longitude, pickup_address, pickup_details, pickup_note,
	dropoff_latitude, dropoff_longitude, dropoff_address, stops, quoted_fare, currency_code,
	passenger_name, passenger_phone, scheduled`

var personalDataQueries = []personalDataQuery{
	{"trips/as_rider.json", "rider", `SELECT jsonb_agg(to_jsonb(x) ORDER BY x.requested_at) FROM (
		SELECT ` + exportTripColumns + ` FROM trips WHERE rider_id = $1::uuid) x`},
	{"trips/as_driver.json", "driver", `SELECT jsonb_agg(to_jsonb(x) ORDER BY x.requested_at) FROM (
		SELECT id, status, requested_at, accepted_at, arrived_at, started_at, completed_at, cancelled_at,
		       cancelled_by, cancellation_reason, rider_no_show, vehicle_class, payment_method,
		       pickup_latitude, pickup_longitude, pickup_address, dropoff_latitude, dropoff_longitude,
		       dropoff_address, stops, quoted_fare, currency_code
		FROM trips WHERE driver_id = $1::uuid) x`},
	{"trips/scheduled.json", "rider", `SELECT jsonb_agg(to_jsonb(x) ORDER BY x.created_at) FROM (
		SELECT id, status, scheduled_at, time_zone, pickup_latitude, pickup_longitude, pickup_address,
		       dropoff_latitude, dropoff_longitude, dropoff_address, stops, vehicle_class, payment_method,
		       passenger_name, passenger_phone, trip_id, created_at, dispatched_at, cancelled_at, failed_at
		FROM scheduled_trips WHERE rider_id = $1::uuid) x`},
	{"trips/ratings_given_as_rider.json", "rider", `SELECT jsonb_agg(to_jsonb(x) ORDER BY x.created_at) FROM (
		SELECT trip_id, stars, comment, created_at FROM trip_ratings WHERE rater_id = $1::uuid) x`},
	{"trips/ratings_given_as_driver.json", "driver", `SELECT jsonb_agg(to_jsonb(x) ORDER BY x.created_at) FROM (
		SELECT trip_id, stars, comment, created_at FROM trip_ratings WHERE rater_id = $1::uuid) x`},
}

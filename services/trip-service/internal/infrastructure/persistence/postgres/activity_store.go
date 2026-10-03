package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/activity"
)

// ActivityStore counts drivers' trips, offers and cancellations.
type ActivityStore struct {
	pool *pgxpool.Pool
}

var _ activity.Store = (*ActivityStore)(nil)

func NewActivityStore(pool *pgxpool.Pool) *ActivityStore {
	if pool == nil {
		panic("postgres pool is required")
	}

	return &ActivityStore{pool: pool}
}

// scopeSQL builds the conditions on a trip alias t and an event time column,
// with its arguments numbered after the ones already in args.
type scopeSQL struct {
	args []any
}

func (b *scopeSQL) arg(value any) string {
	b.args = append(b.args, value)

	return fmt.Sprintf("$%d", len(b.args))
}

// conditions returns "AND ..." for the period on timeColumn and the scope on t.
func (b *scopeSQL) conditions(scope activity.Scope, timeColumn string) string {
	var sql strings.Builder

	fmt.Fprintf(&sql, " AND %s >= %s AND %s < %s", timeColumn, b.arg(scope.From), timeColumn, b.arg(scope.To))

	if scope.CityID != "" {
		fmt.Fprintf(&sql, " AND t.pickup_city_id = %s::uuid", b.arg(scope.CityID))
	}

	if len(scope.ZoneIDs) > 0 {
		fmt.Fprintf(&sql, " AND t.pickup_zone_id = ANY(%s::uuid[])", b.arg(scope.ZoneIDs))
	}

	if scope.VehicleClass != "" {
		fmt.Fprintf(&sql, " AND t.vehicle_class = %s", b.arg(scope.VehicleClass))
	}

	if !scope.AllDay() {
		tz := b.arg(scope.TimeZone)
		minute := fmt.Sprintf("(EXTRACT(HOUR FROM (%[1]s AT TIME ZONE %[2]s)) * 60 + EXTRACT(MINUTE FROM (%[1]s AT TIME ZONE %[2]s)))", timeColumn, tz)
		start, end := b.arg(scope.DailyStart), b.arg(scope.DailyEnd)

		if scope.DailyStart < scope.DailyEnd {
			fmt.Fprintf(&sql, " AND %[1]s >= %[2]s AND %[1]s < %[3]s", minute, start, end)
		} else {
			fmt.Fprintf(&sql, " AND (%[1]s >= %[2]s OR %[1]s < %[3]s)", minute, start, end)
		}
	}

	return sql.String()
}

// counts are the three counts for the driver d.driver_id in scope.
func (b *scopeSQL) counts(scope activity.Scope) string {
	return `
	  (SELECT count(*) FROM trip_offers o JOIN trips t ON t.id = o.trip_id
	    WHERE o.driver_id = d.driver_id AND o.status = 'accepted'` + b.conditions(scope, "o.offered_at") + `),
	  (SELECT count(*) FROM trip_offers o JOIN trips t ON t.id = o.trip_id
	    WHERE o.driver_id = d.driver_id
	      AND (o.status IN ('rejected', 'expired') OR (o.status = 'pending' AND o.expires_at <= now()))` +
		b.conditions(scope, "o.offered_at") + `),
	  (SELECT count(*) FROM trips t
	    WHERE t.driver_id = d.driver_id AND t.cancelled_by = 'driver'` + b.conditions(scope, "t.cancelled_at") + `)`
}

func (s *ActivityStore) Driver(ctx context.Context, driverID string, scope activity.Scope) (activity.Activity, error) {
	b := &scopeSQL{}
	driver := b.arg(driverID)

	sql := `SELECT d.driver_id::text,
	  (SELECT count(*) FROM trips t
	    WHERE t.driver_id = d.driver_id AND t.status = 'completed'` + b.conditions(scope, "t.completed_at") + `),` +
		b.counts(scope) + `
	 FROM (SELECT ` + driver + `::uuid AS driver_id) AS d`

	var a activity.Activity

	if err := s.pool.QueryRow(ctx, sql, b.args...).Scan(
		&a.DriverID, &a.CompletedTrips, &a.OffersAccepted, &a.OffersDeclined, &a.DriverCancellations,
	); err != nil {
		return activity.Activity{}, fmt.Errorf("count driver activity: %w", err)
	}

	return a, nil
}

func (s *ActivityStore) Drivers(ctx context.Context, scope activity.Scope, minCompleted int, afterID string, limit int) ([]activity.Activity, error) {
	b := &scopeSQL{}

	completed := `SELECT t.driver_id, count(*) AS completed
	 FROM trips t
	 WHERE t.status = 'completed' AND t.driver_id IS NOT NULL` + b.conditions(scope, "t.completed_at")

	if afterID != "" {
		completed += ` AND t.driver_id > ` + b.arg(afterID) + `::uuid`
	}

	completed += ` GROUP BY t.driver_id HAVING count(*) >= ` + b.arg(minCompleted) +
		` ORDER BY t.driver_id LIMIT ` + b.arg(limit)

	sql := `WITH d AS (` + completed + `)
	 SELECT d.driver_id::text, d.completed,` + b.counts(scope) + `
	 FROM d ORDER BY d.driver_id`

	rows, err := s.pool.Query(ctx, sql, b.args...)
	if err != nil {
		return nil, fmt.Errorf("count drivers' activity: %w", err)
	}

	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (activity.Activity, error) {
		var a activity.Activity

		return a, row.Scan(&a.DriverID, &a.CompletedTrips, &a.OffersAccepted, &a.OffersDeclined, &a.DriverCancellations)
	})
}

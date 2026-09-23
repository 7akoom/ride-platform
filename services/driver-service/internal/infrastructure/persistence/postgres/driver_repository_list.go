package postgres

import (
	"context"
	"fmt"

	"github.com/7akoom/ride-platform/services/driver-service/internal/application/driver"
)

const driverListColumns = `id, identity_id, display_name, status, availability_status,
	vehicle_make, vehicle_model, vehicle_color, vehicle_plate_number,
	vehicle_class,
	rating_average, rating_count, created_at, updated_at,
	rejection_reason`

// List returns drivers newest first. The cursor is the last driver of the
// previous page, looked up by id, so a page never skips or repeats a driver.
func (r *DriverRepository) List(ctx context.Context, query driver.ListQuery) ([]driver.Driver, error) {
	sql, args := listDriversQuery(query)

	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("list drivers: %w", err)
	}
	defer rows.Close()

	var drivers []driver.Driver

	for rows.Next() {
		var found driver.Driver
		if err := scanDriver(rows, &found); err != nil {
			return nil, fmt.Errorf("scan driver: %w", err)
		}

		drivers = append(drivers, found)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list drivers: %w", err)
	}

	return drivers, nil
}

func listDriversQuery(query driver.ListQuery) (string, []any) {
	sql := `SELECT ` + driverListColumns + ` FROM drivers WHERE TRUE`

	var args []any

	add := func(condition string, value any) {
		args = append(args, value)
		sql += fmt.Sprintf(condition, len(args))
	}

	if query.Status != "" {
		add(` AND status = $%d`, string(query.Status))
	}

	if query.AfterID != "" {
		add(` AND (created_at, id) < (SELECT created_at, id FROM drivers WHERE id = $%d)`, query.AfterID)
	}

	add(` ORDER BY created_at DESC, id DESC LIMIT $%d`, query.Limit)

	return sql, args
}

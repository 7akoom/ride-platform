package postgres

import (
	"context"
	"fmt"

	driverv1 "github.com/7akoom/ride-platform/gen/go/ride/driver/v1"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DriverSupply counts drivers for the business reports.
type DriverSupply struct {
	pool *pgxpool.Pool
}

func NewDriverSupply(pool *pgxpool.Pool) *DriverSupply {
	if pool == nil {
		panic("database pool is required")
	}

	return &DriverSupply{pool: pool}
}

// Count returns how many drivers there are now by status, availability and
// class.
func (s *DriverSupply) Count(ctx context.Context) ([]*driverv1.DriverSupplyCount, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT status, availability_status, vehicle_class, count(*)
		FROM drivers
		GROUP BY 1, 2, 3
		ORDER BY 1, 2, 3`)
	if err != nil {
		return nil, fmt.Errorf("count drivers: %w", err)
	}
	defer rows.Close()

	var out []*driverv1.DriverSupplyCount

	for rows.Next() {
		count := &driverv1.DriverSupplyCount{}
		if err := rows.Scan(&count.Status, &count.Availability, &count.VehicleClass, &count.Drivers); err != nil {
			return nil, fmt.Errorf("scan driver count: %w", err)
		}

		out = append(out, count)
	}

	return out, rows.Err()
}

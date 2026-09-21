package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/7akoom/ride-platform/services/driver-service/internal/application/driver"
)

// UpdateStatus changes the driver's status in one statement that also checks
// the current status, so two operators acting at once cannot both succeed.
func (r *DriverRepository) UpdateStatus(
	ctx context.Context,
	input driver.UpdateStatusInput,
) (driver.Driver, error) {
	allowedFrom := make([]string, 0, len(input.AllowedFrom))
	for _, status := range input.AllowedFrom {
		allowedFrom = append(allowedFrom, string(status))
	}

	row := r.pool.QueryRow(
		ctx,
		`UPDATE drivers
         SET status = $2,
             updated_at = CURRENT_TIMESTAMP
         WHERE id = $1
           AND status = ANY($3::text[])
         RETURNING id, identity_id, display_name, status, availability_status,
                   vehicle_make, vehicle_model, vehicle_color, vehicle_plate_number,
                   vehicle_class,
                   rating_average, rating_count, created_at, updated_at`,
		input.DriverID,
		string(input.To),
		allowedFrom,
	)

	var updated driver.Driver

	err := scanDriver(row, &updated)
	if err == nil {
		return updated, nil
	}

	if !errors.Is(err, pgx.ErrNoRows) {
		return driver.Driver{}, fmt.Errorf("update driver status: %w", err)
	}

	// No row changed: either the driver does not exist, or it is in a status
	// this change cannot start from. Tell the two apart.
	current, findErr := r.FindByID(ctx, input.DriverID)
	if findErr != nil {
		return driver.Driver{}, findErr
	}

	// Already where the caller wants it: a retried approval is not an error.
	if current.Status == input.To {
		return current, nil
	}

	return driver.Driver{}, driver.ErrInvalidStatusTransition
}

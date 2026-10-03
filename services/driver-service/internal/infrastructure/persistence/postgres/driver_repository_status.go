package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/7akoom/ride-platform/services/driver-service/internal/application/driver"
)

// statusEvents names the event a status change tells the driver about.
var statusEvents = map[driver.Status]string{
	driver.StatusActive:   "driver.approved",
	driver.StatusRejected: "driver.rejected",
}

// UpdateStatus changes the driver's status in one statement that also checks
// the current status, so two operators acting at once cannot both succeed.
// The change and its event are written together.
func (r *DriverRepository) UpdateStatus(
	ctx context.Context,
	input driver.UpdateStatusInput,
) (driver.Driver, error) {
	allowedFrom := make([]string, 0, len(input.AllowedFrom))
	for _, status := range input.AllowedFrom {
		allowedFrom = append(allowedFrom, string(status))
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return driver.Driver{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	row := tx.QueryRow(
		ctx,
		`UPDATE drivers
         SET status = $2,
             rejection_reason = $4,
             updated_at = CURRENT_TIMESTAMP
         WHERE id = $1
           AND status = ANY($3::text[])
         RETURNING id, identity_id, display_name, status, availability_status,
                   vehicle_make, vehicle_model, vehicle_color, vehicle_plate_number,
                   vehicle_class, COALESCE(vehicle_id::text, ''), COALESCE(vehicle_year, 0),
                   rating_average, rating_count, created_at, updated_at,
		           rejection_reason`,
		input.DriverID,
		string(input.To),
		allowedFrom,
		input.Reason,
	)

	var updated driver.Driver

	err = scanDriver(row, &updated)
	if err == nil {
		// Approving the driver approves the first car reviewed with them.
		if input.To == driver.StatusActive {
			if _, err := tx.Exec(
				ctx,
				`UPDATE vehicles
				 SET status = 'approved', rejection_reason = '', reviewed_at = CURRENT_TIMESTAMP,
				     updated_at = CURRENT_TIMESTAMP
				 WHERE driver_id = $1 AND active AND status = 'pending'`,
				updated.ID,
			); err != nil {
				return driver.Driver{}, fmt.Errorf("approve the driver's vehicle: %w", err)
			}
		}

		if eventType, tells := statusEvents[input.To]; tells {
			payload := map[string]string{"driver_id": updated.ID, "reason": updated.RejectionReason}

			if err := writeOutboxEvent(ctx, tx, updated.ID, eventType, payload); err != nil {
				return driver.Driver{}, err
			}
		}

		if err := tx.Commit(ctx); err != nil {
			return driver.Driver{}, fmt.Errorf("commit transaction: %w", err)
		}

		return updated, nil
	}

	if !errors.Is(err, pgx.ErrNoRows) {
		return driver.Driver{}, fmt.Errorf("update driver status: %w", err)
	}

	_ = tx.Rollback(ctx)

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

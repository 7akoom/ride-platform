package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/7akoom/ride-platform/services/driver-service/internal/application/driver"
)

const uniqueViolationCode = "23505"

const driverCreatedEventType = "driver.created"

const driverCreatedSchemaVersion = 1

type DriverRepository struct {
	pool *pgxpool.Pool
}

func NewDriverRepository(pool *pgxpool.Pool) *DriverRepository {
	if pool == nil {
		panic("postgres pool is required")
	}

	return &DriverRepository{pool: pool}
}

func (r *DriverRepository) Create(
	ctx context.Context,
	input driver.CreateInput,
) (driver.Driver, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return driver.Driver{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var created driver.Driver

	row := tx.QueryRow(
		ctx,
		`INSERT INTO drivers
		    (id, identity_id, display_name,
		     vehicle_make, vehicle_model, vehicle_color, vehicle_plate_number,
		     vehicle_class, vehicle_id, vehicle_year)
		 VALUES ($1, $2, $3, $4, $5, $6, $7,
		         COALESCE(NULLIF($8::text, ''), 'economy'), $9, NULLIF($10::int, 0))
		 RETURNING id, identity_id, display_name, status, availability_status,
		           vehicle_make, vehicle_model, vehicle_color, vehicle_plate_number,
		           vehicle_class, COALESCE(vehicle_id::text, ''), COALESCE(vehicle_year, 0),
		           rating_average, rating_count, created_at, updated_at,
		           rejection_reason`,
		input.ID,
		input.IdentityID,
		input.DisplayName,
		input.Vehicle.Make,
		input.Vehicle.Model,
		input.Vehicle.Color,
		input.Vehicle.PlateNumber,
		string(input.Vehicle.Class),
		input.VehicleID,
		input.Vehicle.Year,
	)

	if err := scanDriver(row, &created); err != nil {
		var pgErr *pgconn.PgError

		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode {
			if pgErr.ConstraintName == "drivers_plate_number_unique" {
				return driver.Driver{}, driver.ErrPlateNumberTaken
			}

			return driver.Driver{}, driver.ErrDriverAlreadyExists
		}

		return driver.Driver{}, fmt.Errorf("insert driver: %w", err)
	}

	// The first car, active and waiting with the driver's own review.
	if _, err := tx.Exec(
		ctx,
		`INSERT INTO vehicles
		    (id, driver_id, make, model, color, plate_number, year, vehicle_class, status, active)
		 VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7::int, 0), $8, 'pending', TRUE)`,
		input.VehicleID, created.ID,
		created.Vehicle.Make, created.Vehicle.Model, created.Vehicle.Color, created.Vehicle.PlateNumber,
		created.Vehicle.Year, string(created.Vehicle.Class),
	); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode {
			return driver.Driver{}, driver.ErrPlateNumberTaken
		}

		return driver.Driver{}, fmt.Errorf("insert the driver's first vehicle: %w", err)
	}

	payload, err := json.Marshal(map[string]string{
		"driver_id":    created.ID,
		"identity_id":  created.IdentityID,
		"display_name": created.DisplayName,
	})
	if err != nil {
		return driver.Driver{}, fmt.Errorf("marshal driver.created payload: %w", err)
	}

	now := time.Now().UTC()

	if _, err := tx.Exec(
		ctx,
		`INSERT INTO outbox_events
		    (aggregate_type, aggregate_id, event_type, schema_version,
		     payload, occurred_at, available_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $6)`,
		"driver",
		created.ID,
		driverCreatedEventType,
		driverCreatedSchemaVersion,
		payload,
		now,
	); err != nil {
		return driver.Driver{}, fmt.Errorf("insert driver.created outbox event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return driver.Driver{}, fmt.Errorf("commit transaction: %w", err)
	}

	return created, nil
}

func (r *DriverRepository) FindByID(
	ctx context.Context,
	driverID string,
) (driver.Driver, error) {
	row := r.pool.QueryRow(
		ctx,
		`SELECT id, identity_id, display_name, status, availability_status,
		        vehicle_make, vehicle_model, vehicle_color, vehicle_plate_number,
		        vehicle_class, COALESCE(vehicle_id::text, ''), COALESCE(vehicle_year, 0),
		        rating_average, rating_count, created_at, updated_at,
		           rejection_reason
		 FROM drivers
		 WHERE id = $1`,
		driverID,
	)

	var found driver.Driver

	if err := scanDriver(row, &found); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return driver.Driver{}, driver.ErrDriverNotFound
		}

		return driver.Driver{}, fmt.Errorf("select driver by id: %w", err)
	}

	return found, nil
}

func (r *DriverRepository) FindByIdentityID(
	ctx context.Context,
	identityID string,
) (driver.Driver, error) {
	row := r.pool.QueryRow(
		ctx,
		`SELECT id, identity_id, display_name, status, availability_status,
		        vehicle_make, vehicle_model, vehicle_color, vehicle_plate_number,
		        vehicle_class, COALESCE(vehicle_id::text, ''), COALESCE(vehicle_year, 0),
		        rating_average, rating_count, created_at, updated_at,
		           rejection_reason
		 FROM drivers
		 WHERE identity_id = $1`,
		identityID,
	)

	var found driver.Driver

	if err := scanDriver(row, &found); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return driver.Driver{}, driver.ErrDriverNotFound
		}

		return driver.Driver{}, fmt.Errorf("select driver by identity id: %w", err)
	}

	return found, nil
}

// UpdateProfile treats an empty vehicle class as "leave it unchanged", so
// editing a name or plate can never silently downgrade a comfort driver.
func (r *DriverRepository) UpdateProfile(
	ctx context.Context,
	input driver.UpdateProfileInput,
) (driver.Driver, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return driver.Driver{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	row := tx.QueryRow(
		ctx,
		`UPDATE drivers
		 SET display_name = $2,
		     vehicle_make = $3,
		     vehicle_model = $4,
		     vehicle_color = $5,
		     vehicle_plate_number = $6,
		     vehicle_class = COALESCE(NULLIF($7::text, ''), vehicle_class),
		     vehicle_year = COALESCE(NULLIF($8::int, 0), vehicle_year),
		     updated_at = CURRENT_TIMESTAMP
		 WHERE id = $1
		 RETURNING id, identity_id, display_name, status, availability_status,
		           vehicle_make, vehicle_model, vehicle_color, vehicle_plate_number,
		           vehicle_class, COALESCE(vehicle_id::text, ''), COALESCE(vehicle_year, 0),
		           rating_average, rating_count, created_at, updated_at,
		           rejection_reason`,
		input.DriverID,
		input.DisplayName,
		input.Vehicle.Make,
		input.Vehicle.Model,
		input.Vehicle.Color,
		input.Vehicle.PlateNumber,
		string(input.Vehicle.Class),
		input.Vehicle.Year,
	)

	var updated driver.Driver

	if err := scanDriver(row, &updated); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return driver.Driver{}, driver.ErrDriverNotFound
		}

		var pgErr *pgconn.PgError

		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode {
			return driver.Driver{}, driver.ErrPlateNumberTaken
		}

		return driver.Driver{}, fmt.Errorf("update driver profile: %w", err)
	}

	// A car still under review follows the profile; an approved one only
	// changes through staff.
	if _, err := tx.Exec(
		ctx,
		`UPDATE vehicles
		 SET make = $2, model = $3, color = $4, plate_number = $5,
		     vehicle_class = $6, year = NULLIF($7::int, 0), updated_at = CURRENT_TIMESTAMP
		 WHERE driver_id = $1 AND active AND status IN ('pending', 'rejected')`,
		updated.ID,
		updated.Vehicle.Make, updated.Vehicle.Model, updated.Vehicle.Color, updated.Vehicle.PlateNumber,
		string(updated.Vehicle.Class), updated.Vehicle.Year,
	); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode {
			return driver.Driver{}, driver.ErrPlateNumberTaken
		}

		return driver.Driver{}, fmt.Errorf("update the driver's vehicle: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return driver.Driver{}, fmt.Errorf("commit transaction: %w", err)
	}

	return updated, nil
}

func (r *DriverRepository) UpdateAvailability(
	ctx context.Context,
	input driver.UpdateAvailabilityInput,
) (driver.Driver, error) {
	row := r.pool.QueryRow(
		ctx,
		`UPDATE drivers
		 SET availability_status = $2,
		     updated_at = CURRENT_TIMESTAMP
		 WHERE id = $1
		 RETURNING id, identity_id, display_name, status, availability_status,
		           vehicle_make, vehicle_model, vehicle_color, vehicle_plate_number,
		           vehicle_class, COALESCE(vehicle_id::text, ''), COALESCE(vehicle_year, 0),
		           rating_average, rating_count, created_at, updated_at,
		           rejection_reason`,
		input.DriverID,
		string(input.AvailabilityStatus),
	)

	var updated driver.Driver

	if err := scanDriver(row, &updated); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return driver.Driver{}, driver.ErrDriverNotFound
		}

		return driver.Driver{}, fmt.Errorf("update driver availability: %w", err)
	}

	return updated, nil
}

func scanDriver(row pgx.Row, dest *driver.Driver) error {
	var status, availability, vehicleClass string

	err := row.Scan(
		&dest.ID,
		&dest.IdentityID,
		&dest.DisplayName,
		&status,
		&availability,
		&dest.Vehicle.Make,
		&dest.Vehicle.Model,
		&dest.Vehicle.Color,
		&dest.Vehicle.PlateNumber,
		&vehicleClass,
		&dest.Vehicle.ID,
		&dest.Vehicle.Year,
		&dest.RatingAverage,
		&dest.RatingCount,
		&dest.CreatedAt,
		&dest.UpdatedAt,
		&dest.RejectionReason,
	)
	if err != nil {
		return err
	}

	dest.Status = driver.Status(status)
	dest.AvailabilityStatus = driver.AvailabilityStatus(availability)
	dest.Vehicle.Class = driver.VehicleClass(vehicleClass)

	return nil
}

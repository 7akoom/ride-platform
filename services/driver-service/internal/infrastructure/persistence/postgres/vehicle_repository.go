package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/7akoom/ride-platform/services/driver-service/internal/application/documents"
	"github.com/7akoom/ride-platform/services/driver-service/internal/application/vehicles"
)

const eventVehicleReviewed = "driver.vehicle_reviewed"

const vehicleColumns = `v.id::text, v.driver_id::text, v.make, v.model, v.color, v.plate_number,
	COALESCE(v.year, 0), v.vehicle_class, v.status, v.active, v.rejection_reason,
	COALESCE(v.reviewed_by::text, ''), v.reviewed_at, v.created_at, v.updated_at`

// VehicleRepository keeps drivers' cars; the active one is copied to the
// driver row in the same transaction.
type VehicleRepository struct {
	pool *pgxpool.Pool
}

var (
	_ vehicles.Repository = (*VehicleRepository)(nil)
	_ documents.Vehicles  = (*VehicleRepository)(nil)
)

func NewVehicleRepository(pool *pgxpool.Pool) *VehicleRepository {
	if pool == nil {
		panic("postgres pool is required")
	}

	return &VehicleRepository{pool: pool}
}

func (r *VehicleRepository) Add(ctx context.Context, v vehicles.Vehicle, maxInService int) (vehicles.Vehicle, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return vehicles.Vehicle{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := lockDriver(ctx, tx, v.DriverID); err != nil {
		return vehicles.Vehicle{}, err
	}

	var inService int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM vehicles WHERE driver_id = $1 AND status <> 'retired'`, v.DriverID,
	).Scan(&inService); err != nil {
		return vehicles.Vehicle{}, fmt.Errorf("count vehicles: %w", err)
	}

	if inService >= maxInService {
		return vehicles.Vehicle{}, vehicles.ErrTooManyVehicles
	}

	var added vehicles.Vehicle

	err = scanVehicle(tx.QueryRow(
		ctx,
		`INSERT INTO vehicles AS v
		    (id, driver_id, make, model, color, plate_number, year, vehicle_class, status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'pending')
		 RETURNING `+vehicleColumns,
		v.ID, v.DriverID, v.Make, v.Model, v.Color, v.PlateNumber, v.Year, v.Class,
	), &added)
	if err != nil {
		return vehicles.Vehicle{}, plateError(err, "insert vehicle")
	}

	if err := tx.Commit(ctx); err != nil {
		return vehicles.Vehicle{}, fmt.Errorf("commit transaction: %w", err)
	}

	return added, nil
}

func (r *VehicleRepository) Get(ctx context.Context, id string) (vehicles.Vehicle, error) {
	var found vehicles.Vehicle

	err := scanVehicle(r.pool.QueryRow(ctx, `SELECT `+vehicleColumns+` FROM vehicles AS v WHERE v.id = $1`, id), &found)
	if errors.Is(err, pgx.ErrNoRows) {
		return vehicles.Vehicle{}, vehicles.ErrVehicleNotFound
	}

	if err != nil {
		return vehicles.Vehicle{}, fmt.Errorf("select vehicle: %w", err)
	}

	return found, nil
}

func (r *VehicleRepository) ListByDriver(ctx context.Context, driverID string) ([]vehicles.Vehicle, error) {
	rows, err := r.pool.Query(
		ctx,
		`SELECT `+vehicleColumns+`
		 FROM vehicles AS v
		 WHERE v.driver_id = $1
		 ORDER BY v.active DESC, (v.status = 'retired'), v.created_at, v.id`,
		driverID,
	)
	if err != nil {
		return nil, fmt.Errorf("select vehicles: %w", err)
	}

	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (vehicles.Vehicle, error) {
		var v vehicles.Vehicle

		return v, scanVehicle(row, &v)
	})
}

func (r *VehicleRepository) Update(ctx context.Context, v vehicles.Vehicle) (vehicles.Vehicle, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return vehicles.Vehicle{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var updated vehicles.Vehicle

	err = scanVehicle(tx.QueryRow(
		ctx,
		`UPDATE vehicles AS v
		 SET make = $2, model = $3, color = $4, plate_number = $5, year = $6, vehicle_class = $7,
		     status = 'pending', rejection_reason = '', updated_at = CURRENT_TIMESTAMP
		 WHERE v.id = $1 AND v.status IN ('pending', 'rejected')
		 RETURNING `+vehicleColumns,
		v.ID, v.Make, v.Model, v.Color, v.PlateNumber, v.Year, v.Class,
	), &updated)
	if errors.Is(err, pgx.ErrNoRows) {
		return vehicles.Vehicle{}, vehicles.ErrVehicleNotEditable
	}

	if err != nil {
		return vehicles.Vehicle{}, plateError(err, "update vehicle")
	}

	if updated.Active {
		if err := copyToDriver(ctx, tx, updated); err != nil {
			return vehicles.Vehicle{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return vehicles.Vehicle{}, fmt.Errorf("commit transaction: %w", err)
	}

	return updated, nil
}

func (r *VehicleRepository) Activate(ctx context.Context, driverID, vehicleID string) (vehicles.Vehicle, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return vehicles.Vehicle{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var availability string
	if err := tx.QueryRow(ctx,
		`SELECT availability_status FROM drivers WHERE id = $1 FOR UPDATE`, driverID,
	).Scan(&availability); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return vehicles.Vehicle{}, vehicles.ErrDriverNotFound
		}

		return vehicles.Vehicle{}, fmt.Errorf("lock driver: %w", err)
	}

	current, err := lockVehicle(ctx, tx, vehicleID)
	if err != nil {
		return vehicles.Vehicle{}, err
	}

	switch {
	case current.DriverID != driverID:
		return vehicles.Vehicle{}, vehicles.ErrVehicleNotFound
	case current.Active:
		return current, nil
	case current.Status != vehicles.StatusApproved:
		return vehicles.Vehicle{}, vehicles.ErrVehicleNotApproved
	case availability != "offline":
		return vehicles.Vehicle{}, vehicles.ErrDriverNotOffline
	}

	if _, err := tx.Exec(ctx,
		`UPDATE vehicles SET active = FALSE, updated_at = CURRENT_TIMESTAMP WHERE driver_id = $1 AND active`, driverID,
	); err != nil {
		return vehicles.Vehicle{}, fmt.Errorf("deactivate the current car: %w", err)
	}

	var activated vehicles.Vehicle

	if err := scanVehicle(tx.QueryRow(ctx,
		`UPDATE vehicles AS v SET active = TRUE, updated_at = CURRENT_TIMESTAMP WHERE v.id = $1 RETURNING `+vehicleColumns,
		vehicleID,
	), &activated); err != nil {
		return vehicles.Vehicle{}, fmt.Errorf("activate the car: %w", err)
	}

	if err := copyToDriver(ctx, tx, activated); err != nil {
		return vehicles.Vehicle{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return vehicles.Vehicle{}, fmt.Errorf("commit transaction: %w", err)
	}

	return activated, nil
}

func (r *VehicleRepository) Retire(ctx context.Context, driverID, vehicleID string) (vehicles.Vehicle, []string, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return vehicles.Vehicle{}, nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := lockDriver(ctx, tx, driverID); err != nil {
		return vehicles.Vehicle{}, nil, err
	}

	current, err := lockVehicle(ctx, tx, vehicleID)
	if err != nil {
		return vehicles.Vehicle{}, nil, err
	}

	switch {
	case current.DriverID != driverID:
		return vehicles.Vehicle{}, nil, vehicles.ErrVehicleNotFound
	case current.Status == vehicles.StatusRetired:
		return vehicles.Vehicle{}, nil, vehicles.ErrVehicleRetired
	case current.Active:
		return vehicles.Vehicle{}, nil, vehicles.ErrVehicleActive
	}

	var retired vehicles.Vehicle

	if err := scanVehicle(tx.QueryRow(ctx,
		`UPDATE vehicles AS v
		 SET status = 'retired', retired_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		 WHERE v.id = $1
		 RETURNING `+vehicleColumns,
		vehicleID,
	), &retired); err != nil {
		return vehicles.Vehicle{}, nil, fmt.Errorf("retire the car: %w", err)
	}

	rows, err := tx.Query(ctx,
		`UPDATE driver_documents
		 SET status = 'superseded', superseded_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		 WHERE vehicle_id = $1 AND status <> 'superseded'
		 RETURNING media_id::text`,
		vehicleID,
	)
	if err != nil {
		return vehicles.Vehicle{}, nil, fmt.Errorf("supersede the car's documents: %w", err)
	}

	files, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return vehicles.Vehicle{}, nil, fmt.Errorf("supersede the car's documents: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return vehicles.Vehicle{}, nil, fmt.Errorf("commit transaction: %w", err)
	}

	return retired, files, nil
}

func (r *VehicleRepository) Approve(ctx context.Context, record vehicles.ApproveRecord) (vehicles.Vehicle, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return vehicles.Vehicle{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var approved vehicles.Vehicle

	err = scanVehicle(tx.QueryRow(
		ctx,
		`UPDATE vehicles AS v
		 SET status = 'approved', year = $2, vehicle_class = $3, rejection_reason = '',
		     reviewed_by = NULLIF($4, '')::uuid, reviewed_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		 WHERE v.id = $1 AND v.status = 'pending'
		 RETURNING `+vehicleColumns,
		record.VehicleID, record.Year, record.Class, record.ReviewedBy,
	), &approved)
	if errors.Is(err, pgx.ErrNoRows) {
		return vehicles.Vehicle{}, vehicles.ErrVehicleNotPending
	}

	if err != nil {
		return vehicles.Vehicle{}, fmt.Errorf("approve vehicle: %w", err)
	}

	if approved.Active {
		if err := copyToDriver(ctx, tx, approved); err != nil {
			return vehicles.Vehicle{}, err
		}
	}

	if err := writeOutboxEvent(ctx, tx, approved.DriverID, eventVehicleReviewed, vehiclePayload(approved, "approved", "")); err != nil {
		return vehicles.Vehicle{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return vehicles.Vehicle{}, fmt.Errorf("commit transaction: %w", err)
	}

	return approved, nil
}

func (r *VehicleRepository) Reject(ctx context.Context, record vehicles.RejectRecord) (vehicles.Vehicle, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return vehicles.Vehicle{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var rejected vehicles.Vehicle

	err = scanVehicle(tx.QueryRow(
		ctx,
		`UPDATE vehicles AS v
		 SET status = 'rejected', rejection_reason = $2,
		     reviewed_by = NULLIF($3, '')::uuid, reviewed_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		 WHERE v.id = $1 AND v.status = 'pending'
		 RETURNING `+vehicleColumns,
		record.VehicleID, record.Reason, record.ReviewedBy,
	), &rejected)
	if errors.Is(err, pgx.ErrNoRows) {
		return vehicles.Vehicle{}, vehicles.ErrVehicleNotPending
	}

	if err != nil {
		return vehicles.Vehicle{}, fmt.Errorf("reject vehicle: %w", err)
	}

	if err := writeOutboxEvent(ctx, tx, rejected.DriverID, eventVehicleReviewed, vehiclePayload(rejected, "rejected", record.Reason)); err != nil {
		return vehicles.Vehicle{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return vehicles.Vehicle{}, fmt.Errorf("commit transaction: %w", err)
	}

	return rejected, nil
}

func (r *VehicleRepository) ListPending(ctx context.Context, query vehicles.PendingQuery) ([]vehicles.PendingItem, error) {
	after := query.AfterCreatedAt
	if after.IsZero() {
		after = time.Unix(0, 0).UTC()
	}

	afterID := query.AfterID
	if afterID == "" {
		afterID = "00000000-0000-0000-0000-000000000000"
	}

	rows, err := r.pool.Query(
		ctx,
		`SELECT `+vehicleColumns+`, dr.display_name, dr.status
		 FROM vehicles AS v
		 JOIN drivers AS dr ON dr.id = v.driver_id
		 WHERE v.status = 'pending' AND (v.created_at, v.id) > ($1, $2::uuid)
		 ORDER BY v.created_at, v.id
		 LIMIT $3`,
		after, afterID, query.Limit,
	)
	if err != nil {
		return nil, fmt.Errorf("select pending vehicles: %w", err)
	}

	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (vehicles.PendingItem, error) {
		var item vehicles.PendingItem

		return item, scanVehicle(row, &item.Vehicle, &item.DriverDisplayName, &item.DriverStatus)
	})
}

// --- what documents need ---------------------------------------------------------

func (r *VehicleRepository) ActiveVehicle(ctx context.Context, driverID string) (documents.VehicleRef, bool, error) {
	var ref documents.VehicleRef

	err := r.pool.QueryRow(ctx,
		`SELECT id::text, driver_id::text, status, active FROM vehicles WHERE driver_id = $1 AND active`, driverID,
	).Scan(&ref.ID, &ref.DriverID, &ref.Status, &ref.Active)
	if errors.Is(err, pgx.ErrNoRows) {
		return documents.VehicleRef{}, false, nil
	}

	if err != nil {
		return documents.VehicleRef{}, false, fmt.Errorf("select the active car: %w", err)
	}

	return ref, true, nil
}

func (r *VehicleRepository) Vehicle(ctx context.Context, vehicleID string) (documents.VehicleRef, error) {
	var ref documents.VehicleRef

	err := r.pool.QueryRow(ctx,
		`SELECT id::text, driver_id::text, status, active FROM vehicles WHERE id = $1`, vehicleID,
	).Scan(&ref.ID, &ref.DriverID, &ref.Status, &ref.Active)
	if errors.Is(err, pgx.ErrNoRows) {
		return documents.VehicleRef{}, documents.ErrVehicleNotFound
	}

	if err != nil {
		return documents.VehicleRef{}, fmt.Errorf("select car: %w", err)
	}

	return ref, nil
}

// --- helpers -------------------------------------------------------------------------

func lockDriver(ctx context.Context, tx pgx.Tx, driverID string) error {
	var id string

	err := tx.QueryRow(ctx, `SELECT id::text FROM drivers WHERE id = $1 FOR UPDATE`, driverID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return vehicles.ErrDriverNotFound
	}

	if err != nil {
		return fmt.Errorf("lock driver: %w", err)
	}

	return nil
}

func lockVehicle(ctx context.Context, tx pgx.Tx, vehicleID string) (vehicles.Vehicle, error) {
	var v vehicles.Vehicle

	err := scanVehicle(tx.QueryRow(ctx, `SELECT `+vehicleColumns+` FROM vehicles AS v WHERE v.id = $1 FOR UPDATE`, vehicleID), &v)
	if errors.Is(err, pgx.ErrNoRows) {
		return vehicles.Vehicle{}, vehicles.ErrVehicleNotFound
	}

	if err != nil {
		return vehicles.Vehicle{}, fmt.Errorf("lock vehicle: %w", err)
	}

	return v, nil
}

// copyToDriver keeps the driver row's copy of the active car current: what
// riders are shown and what dispatch and pricing read.
func copyToDriver(ctx context.Context, tx pgx.Tx, v vehicles.Vehicle) error {
	if _, err := tx.Exec(
		ctx,
		`UPDATE drivers
		 SET vehicle_id = $2, vehicle_make = $3, vehicle_model = $4, vehicle_color = $5,
		     vehicle_plate_number = $6, vehicle_class = $7, vehicle_year = NULLIF($8::int, 0),
		     updated_at = CURRENT_TIMESTAMP
		 WHERE id = $1`,
		v.DriverID, v.ID, v.Make, v.Model, v.Color, v.PlateNumber, v.Class, v.Year,
	); err != nil {
		return plateError(err, "copy the active car to the driver")
	}

	return nil
}

func plateError(err error, doing string) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode {
		return vehicles.ErrPlateTaken
	}

	return fmt.Errorf("%s: %w", doing, err)
}

func scanVehicle(row pgx.Row, v *vehicles.Vehicle, extra ...any) error {
	var (
		status     string
		reviewedAt *time.Time
	)

	dest := []any{
		&v.ID, &v.DriverID, &v.Make, &v.Model, &v.Color, &v.PlateNumber,
		&v.Year, &v.Class, &status, &v.Active, &v.RejectionReason,
		&v.ReviewedBy, &reviewedAt, &v.CreatedAt, &v.UpdatedAt,
	}

	if err := row.Scan(append(dest, extra...)...); err != nil {
		return err
	}

	v.Status = vehicles.Status(status)

	if reviewedAt != nil {
		v.ReviewedAt = *reviewedAt
	}

	return nil
}

func vehiclePayload(v vehicles.Vehicle, decision, reason string) map[string]any {
	return map[string]any{
		"driver_id":    v.DriverID,
		"vehicle_id":   v.ID,
		"plate_number": v.PlateNumber,
		"make":         v.Make,
		"model":        v.Model,
		"decision":     decision,
		"reason":       reason,
	}
}

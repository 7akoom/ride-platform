package postgres

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/7akoom/ride-platform/services/driver-service/internal/application/profile"
)

// NameChangeReviewedEvent tells the driver how a name change was decided.
const NameChangeReviewedEvent = "driver.name_change_reviewed"

// ProfileRepository keeps drivers' personal details and name changes.
type ProfileRepository struct {
	pool *pgxpool.Pool
}

var _ profile.Repository = (*ProfileRepository)(nil)

func NewProfileRepository(pool *pgxpool.Pool) *ProfileRepository {
	if pool == nil {
		panic("postgres pool is required")
	}

	return &ProfileRepository{pool: pool}
}

var uuidShape = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func (r *ProfileRepository) GetDetails(ctx context.Context, driverID string) (profile.Details, error) {
	d := profile.Details{DriverID: driverID}

	var updated *time.Time

	err := r.pool.QueryRow(ctx,
		`SELECT d.gender, COALESCE(to_char(d.date_of_birth, 'YYYY-MM-DD'), ''), d.nationality, d.updated_at
		 FROM driver_details d WHERE d.driver_id = $1`, driverID).
		Scan(&d.Fields.Gender, &d.Fields.DateOfBirth, &d.Fields.Nationality, &updated)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return profile.Details{}, fmt.Errorf("get driver details: %w", err)
	}

	if updated != nil {
		d.UpdatedAt = *updated
	}

	if _, d.HasPhoto, err = r.ApprovedPhoto(ctx, driverID); err != nil {
		return profile.Details{}, err
	}

	return d, nil
}

func dateArg(value string) any {
	if value == "" {
		return nil
	}

	return value
}

func (r *ProfileRepository) SaveFields(ctx context.Context, driverID string, f profile.Fields, at time.Time) (profile.Details, error) {
	if _, err := r.pool.Exec(ctx,
		`INSERT INTO driver_details (driver_id, gender, date_of_birth, nationality, updated_at)
		 VALUES ($1, $2, $3::date, $4, $5)
		 ON CONFLICT (driver_id) DO UPDATE SET
		    gender = EXCLUDED.gender, date_of_birth = EXCLUDED.date_of_birth,
		    nationality = EXCLUDED.nationality, updated_at = EXCLUDED.updated_at`,
		driverID, f.Gender, dateArg(f.DateOfBirth), f.Nationality, at); err != nil {
		return profile.Details{}, fmt.Errorf("save driver details: %w", err)
	}

	return r.GetDetails(ctx, driverID)
}

func (r *ProfileRepository) ApprovedPhoto(ctx context.Context, driverID string) (string, bool, error) {
	var mediaID string

	err := r.pool.QueryRow(ctx,
		`SELECT media_id::text FROM driver_documents
		 WHERE driver_id = $1 AND type_code = 'profile_photo' AND status = 'approved'
		 ORDER BY reviewed_at DESC NULLS LAST, created_at DESC
		 LIMIT 1`, driverID).Scan(&mediaID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}

	if err != nil {
		return "", false, fmt.Errorf("find approved profile photo: %w", err)
	}

	return mediaID, true, nil
}

const nameChangeColumns = `id, driver_id, current_name, requested_name, reason, status, rejection_reason,
	COALESCE(decided_by::text, ''), created_at, decided_at`

func scanNameChange(row pgx.Row) (profile.NameChange, error) {
	var c profile.NameChange
	var status string

	err := row.Scan(&c.ID, &c.DriverID, &c.CurrentName, &c.RequestedName, &c.Reason, &status,
		&c.RejectionReason, &c.DecidedBy, &c.CreatedAt, &c.DecidedAt)
	c.Status = profile.NameChangeStatus(status)

	return c, err
}

func (r *ProfileRepository) CreateNameChange(ctx context.Context, c profile.NameChange) (profile.NameChange, error) {
	saved, err := scanNameChange(r.pool.QueryRow(ctx,
		`INSERT INTO driver_name_changes (id, driver_id, current_name, requested_name, reason, status, created_at)
		 VALUES ($1, $2, $3, $4, $5, 'pending', $6)
		 RETURNING `+nameChangeColumns,
		c.ID, c.DriverID, c.CurrentName, c.RequestedName, c.Reason, c.CreatedAt))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode {
			return profile.NameChange{}, profile.ErrNameChangeOpen
		}

		return profile.NameChange{}, fmt.Errorf("create name change: %w", err)
	}

	return saved, nil
}

func (r *ProfileRepository) queryNameChanges(ctx context.Context, query string, args ...any) ([]profile.NameChange, error) {
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list name changes: %w", err)
	}
	defer rows.Close()

	var out []profile.NameChange

	for rows.Next() {
		c, err := scanNameChange(rows)
		if err != nil {
			return nil, fmt.Errorf("scan name change: %w", err)
		}

		out = append(out, c)
	}

	return out, rows.Err()
}

func (r *ProfileRepository) ListNameChanges(ctx context.Context, driverID string) ([]profile.NameChange, error) {
	return r.queryNameChanges(ctx,
		`SELECT `+nameChangeColumns+` FROM driver_name_changes WHERE driver_id = $1 ORDER BY created_at DESC LIMIT 50`, driverID)
}

func (r *ProfileRepository) ListPendingNameChanges(ctx context.Context, limit int) ([]profile.NameChange, error) {
	return r.queryNameChanges(ctx,
		`SELECT `+nameChangeColumns+` FROM driver_name_changes WHERE status = 'pending' ORDER BY created_at, id LIMIT $1`, limit)
}

func (r *ProfileRepository) DecideNameChange(
	ctx context.Context,
	id string,
	decide func(change *profile.NameChange) error,
) (profile.NameChange, error) {
	if !uuidShape.MatchString(id) {
		return profile.NameChange{}, profile.ErrNameChangeNotFound
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return profile.NameChange{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	c, err := scanNameChange(tx.QueryRow(ctx, `SELECT `+nameChangeColumns+` FROM driver_name_changes WHERE id = $1 FOR UPDATE`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return profile.NameChange{}, profile.ErrNameChangeNotFound
	}

	if err != nil {
		return profile.NameChange{}, fmt.Errorf("lock name change: %w", err)
	}

	if err := decide(&c); err != nil {
		return profile.NameChange{}, err
	}

	var decidedBy any
	if uuidShape.MatchString(c.DecidedBy) {
		decidedBy = c.DecidedBy
	}

	if _, err := tx.Exec(ctx,
		`UPDATE driver_name_changes SET status = $2, rejection_reason = $3, decided_by = $4, decided_at = $5 WHERE id = $1`,
		c.ID, string(c.Status), c.RejectionReason, decidedBy, c.DecidedAt); err != nil {
		return profile.NameChange{}, fmt.Errorf("decide name change: %w", err)
	}

	if c.Status == profile.NameChangeApproved {
		if _, err := tx.Exec(ctx,
			`UPDATE drivers SET display_name = $2, updated_at = now() WHERE id = $1`, c.DriverID, c.RequestedName); err != nil {
			return profile.NameChange{}, fmt.Errorf("rename driver: %w", err)
		}
	}

	if err := writeOutboxEvent(ctx, tx, c.DriverID, NameChangeReviewedEvent, map[string]string{
		"driver_id":      c.DriverID,
		"name_change_id": c.ID,
		"decision":       string(c.Status),
		"requested_name": c.RequestedName,
		"reason":         c.RejectionReason,
	}); err != nil {
		return profile.NameChange{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return profile.NameChange{}, fmt.Errorf("commit name change decision: %w", err)
	}

	return c, nil
}

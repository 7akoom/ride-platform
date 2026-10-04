package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/7akoom/ride-platform/services/rider-service/internal/application/profile"
)

// DetailsRepository keeps riders' personal details and pictures.
type DetailsRepository struct {
	pool *pgxpool.Pool
}

var _ profile.Repository = (*DetailsRepository)(nil)

func NewDetailsRepository(pool *pgxpool.Pool) *DetailsRepository {
	if pool == nil {
		panic("postgres pool is required")
	}

	return &DetailsRepository{pool: pool}
}

const detailsColumns = `rider_id, gender, COALESCE(to_char(date_of_birth, 'YYYY-MM-DD'), ''), nationality,
	COALESCE(photo_media_id::text, ''), updated_at`

func scanDetails(row pgx.Row) (profile.Details, error) {
	var d profile.Details

	err := row.Scan(&d.RiderID, &d.Fields.Gender, &d.Fields.DateOfBirth, &d.Fields.Nationality, &d.PhotoMediaID, &d.UpdatedAt)

	return d, err
}

func (r *DetailsRepository) GetDetails(ctx context.Context, riderID string) (profile.Details, error) {
	d, err := scanDetails(r.pool.QueryRow(ctx, `SELECT `+detailsColumns+` FROM rider_details WHERE rider_id = $1`, riderID))
	if errors.Is(err, pgx.ErrNoRows) {
		return profile.Details{RiderID: riderID}, nil
	}

	if err != nil {
		return profile.Details{}, fmt.Errorf("get rider details: %w", err)
	}

	return d, nil
}

func dateArg(value string) any {
	if value == "" {
		return nil
	}

	return value
}

func (r *DetailsRepository) SaveFields(ctx context.Context, riderID string, f profile.Fields, at time.Time) (profile.Details, error) {
	d, err := scanDetails(r.pool.QueryRow(ctx,
		`INSERT INTO rider_details (rider_id, gender, date_of_birth, nationality, updated_at)
		 VALUES ($1, $2, $3::date, $4, $5)
		 ON CONFLICT (rider_id) DO UPDATE SET
		    gender = EXCLUDED.gender, date_of_birth = EXCLUDED.date_of_birth,
		    nationality = EXCLUDED.nationality, updated_at = EXCLUDED.updated_at
		 RETURNING `+detailsColumns,
		riderID, f.Gender, dateArg(f.DateOfBirth), f.Nationality, at))
	if err != nil {
		return profile.Details{}, fmt.Errorf("save rider details: %w", err)
	}

	return d, nil
}

func (r *DetailsRepository) SetPhoto(ctx context.Context, riderID, mediaID string, at time.Time) (profile.Details, string, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return profile.Details{}, "", fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var old string

	err = tx.QueryRow(ctx,
		`SELECT COALESCE(photo_media_id::text, '') FROM rider_details WHERE rider_id = $1 FOR UPDATE`, riderID).Scan(&old)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return profile.Details{}, "", fmt.Errorf("lock rider details: %w", err)
	}

	var photo any
	if mediaID != "" {
		photo = mediaID
	}

	d, err := scanDetails(tx.QueryRow(ctx,
		`INSERT INTO rider_details (rider_id, photo_media_id, updated_at)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (rider_id) DO UPDATE SET photo_media_id = EXCLUDED.photo_media_id, updated_at = EXCLUDED.updated_at
		 RETURNING `+detailsColumns, riderID, photo, at))
	if err != nil {
		return profile.Details{}, "", fmt.Errorf("set rider photo: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return profile.Details{}, "", fmt.Errorf("commit rider photo: %w", err)
	}

	if old == mediaID {
		old = ""
	}

	return d, old, nil
}

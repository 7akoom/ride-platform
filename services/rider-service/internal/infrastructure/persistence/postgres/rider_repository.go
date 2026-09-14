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

	"github.com/7akoom/ride-platform/services/rider-service/internal/application/rider"
)

const uniqueViolationCode = "23505"

const riderCreatedEventType = "rider.created"

const riderCreatedSchemaVersion = 1

type RiderRepository struct {
	pool *pgxpool.Pool
}

func NewRiderRepository(pool *pgxpool.Pool) *RiderRepository {
	if pool == nil {
		panic("postgres pool is required")
	}

	return &RiderRepository{pool: pool}
}

func (r *RiderRepository) Create(
	ctx context.Context,
	input rider.CreateInput,
) (rider.Rider, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return rider.Rider{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var created rider.Rider

	row := tx.QueryRow(
		ctx,
		`INSERT INTO riders (id, identity_id, display_name)
		 VALUES ($1, $2, $3)
		 RETURNING id, identity_id, display_name, status,
		           rating_average, rating_count, created_at, updated_at`,
		input.ID,
		input.IdentityID,
		input.DisplayName,
	)

	if err := scanRider(row, &created); err != nil {
		var pgErr *pgconn.PgError

		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode {
			return rider.Rider{}, rider.ErrRiderAlreadyExists
		}

		return rider.Rider{}, fmt.Errorf("insert rider: %w", err)
	}

	payload, err := json.Marshal(map[string]string{
		"rider_id":     created.ID,
		"identity_id":  created.IdentityID,
		"display_name": created.DisplayName,
	})
	if err != nil {
		return rider.Rider{}, fmt.Errorf("marshal rider.created payload: %w", err)
	}

	now := time.Now().UTC()

	if _, err := tx.Exec(
		ctx,
		`INSERT INTO outbox_events
		    (aggregate_type, aggregate_id, event_type, schema_version,
		     payload, occurred_at, available_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $6)`,
		"rider",
		created.ID,
		riderCreatedEventType,
		riderCreatedSchemaVersion,
		payload,
		now,
	); err != nil {
		return rider.Rider{}, fmt.Errorf("insert rider.created outbox event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return rider.Rider{}, fmt.Errorf("commit transaction: %w", err)
	}

	return created, nil
}

func (r *RiderRepository) FindByID(
	ctx context.Context,
	riderID string,
) (rider.Rider, error) {
	row := r.pool.QueryRow(
		ctx,
		`SELECT id, identity_id, display_name, status,
		        rating_average, rating_count, created_at, updated_at
		 FROM riders
		 WHERE id = $1`,
		riderID,
	)

	var found rider.Rider

	if err := scanRider(row, &found); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return rider.Rider{}, rider.ErrRiderNotFound
		}

		return rider.Rider{}, fmt.Errorf("select rider by id: %w", err)
	}

	return found, nil
}

func (r *RiderRepository) FindByIdentityID(
	ctx context.Context,
	identityID string,
) (rider.Rider, error) {
	row := r.pool.QueryRow(
		ctx,
		`SELECT id, identity_id, display_name, status,
		        rating_average, rating_count, created_at, updated_at
		 FROM riders
		 WHERE identity_id = $1`,
		identityID,
	)

	var found rider.Rider

	if err := scanRider(row, &found); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return rider.Rider{}, rider.ErrRiderNotFound
		}

		return rider.Rider{}, fmt.Errorf("select rider by identity id: %w", err)
	}

	return found, nil
}

func (r *RiderRepository) UpdateProfile(
	ctx context.Context,
	input rider.UpdateProfileInput,
) (rider.Rider, error) {
	row := r.pool.QueryRow(
		ctx,
		`UPDATE riders
		 SET display_name = $2,
		     updated_at = CURRENT_TIMESTAMP
		 WHERE id = $1
		 RETURNING id, identity_id, display_name, status,
		           rating_average, rating_count, created_at, updated_at`,
		input.RiderID,
		input.DisplayName,
	)

	var updated rider.Rider

	if err := scanRider(row, &updated); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return rider.Rider{}, rider.ErrRiderNotFound
		}

		return rider.Rider{}, fmt.Errorf("update rider profile: %w", err)
	}

	return updated, nil
}

func scanRider(row pgx.Row, dest *rider.Rider) error {
	var status string

	err := row.Scan(
		&dest.ID,
		&dest.IdentityID,
		&dest.DisplayName,
		&status,
		&dest.RatingAverage,
		&dest.RatingCount,
		&dest.CreatedAt,
		&dest.UpdatedAt,
	)
	if err != nil {
		return err
	}

	dest.Status = rider.Status(status)

	return nil
}

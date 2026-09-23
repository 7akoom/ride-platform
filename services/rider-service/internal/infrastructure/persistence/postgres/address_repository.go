package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/7akoom/ride-platform/services/rider-service/internal/application/address"
)

type AddressRepository struct {
	pool *pgxpool.Pool
}

var _ address.Repository = (*AddressRepository)(nil)

func NewAddressRepository(pool *pgxpool.Pool) *AddressRepository {
	if pool == nil {
		panic("postgres pool is required")
	}

	return &AddressRepository{pool: pool}
}

const addressColumns = `id, rider_id, kind, label, latitude, longitude, address, details, note_for_driver,
	COALESCE(photo_media_id::text, ''), created_at, updated_at`

func scanAddress(row pgx.Row) (address.Address, error) {
	var (
		a    address.Address
		kind string
	)

	if err := row.Scan(
		&a.ID, &a.RiderID, &kind, &a.Label, &a.Coordinates.Latitude, &a.Coordinates.Longitude,
		&a.Address, &a.Details, &a.NoteForDriver, &a.PhotoMediaID, &a.CreatedAt, &a.UpdatedAt,
	); err != nil {
		return address.Address{}, err
	}

	a.Kind = address.Kind(kind)

	return a, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError

	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode
}

// Create checks the rider exists and has room under a lock on the rider's
// row, so two concurrent creates cannot both take the last place.
func (r *AddressRepository) Create(ctx context.Context, a address.Address) (address.Address, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return address.Address{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var locked string

	err = tx.QueryRow(ctx, `SELECT id FROM riders WHERE id = $1 FOR UPDATE`, a.RiderID).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return address.Address{}, address.ErrRiderNotFound
	}

	if err != nil {
		return address.Address{}, fmt.Errorf("lock rider: %w", err)
	}

	var count int

	if err := tx.QueryRow(ctx, `SELECT count(*) FROM saved_addresses WHERE rider_id = $1`, a.RiderID).Scan(&count); err != nil {
		return address.Address{}, fmt.Errorf("count saved addresses: %w", err)
	}

	if count >= address.MaxPerRider {
		return address.Address{}, address.ErrTooManyAddresses
	}

	created, err := scanAddress(tx.QueryRow(ctx, `
		INSERT INTO saved_addresses
		    (id, rider_id, kind, label, latitude, longitude, address, details, note_for_driver, photo_media_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NULLIF($10, '')::uuid)
		RETURNING `+addressColumns,
		a.ID, a.RiderID, string(a.Kind), a.Label, a.Coordinates.Latitude, a.Coordinates.Longitude,
		a.Address, a.Details, a.NoteForDriver, a.PhotoMediaID,
	))
	if isUniqueViolation(err) {
		return address.Address{}, address.ErrKindTaken
	}

	if err != nil {
		return address.Address{}, fmt.Errorf("insert saved address: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return address.Address{}, fmt.Errorf("commit saved address: %w", err)
	}

	return created, nil
}

func (r *AddressRepository) Update(ctx context.Context, a address.Address) (address.Address, error) {
	updated, err := scanAddress(r.pool.QueryRow(ctx, `
		UPDATE saved_addresses
		SET kind = $3, label = $4, latitude = $5, longitude = $6, address = $7, details = $8,
		    note_for_driver = $9, photo_media_id = NULLIF($10, '')::uuid, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND rider_id = $2
		RETURNING `+addressColumns,
		a.ID, a.RiderID, string(a.Kind), a.Label, a.Coordinates.Latitude, a.Coordinates.Longitude,
		a.Address, a.Details, a.NoteForDriver, a.PhotoMediaID,
	))

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return address.Address{}, address.ErrAddressNotFound
	case isUniqueViolation(err):
		return address.Address{}, address.ErrKindTaken
	case err != nil:
		return address.Address{}, fmt.Errorf("update saved address: %w", err)
	}

	return updated, nil
}

func (r *AddressRepository) Get(ctx context.Context, riderID, id string) (address.Address, error) {
	found, err := scanAddress(r.pool.QueryRow(ctx,
		`SELECT `+addressColumns+` FROM saved_addresses WHERE id = $1 AND rider_id = $2`, id, riderID))
	if errors.Is(err, pgx.ErrNoRows) {
		return address.Address{}, address.ErrAddressNotFound
	}

	if err != nil {
		return address.Address{}, fmt.Errorf("get saved address: %w", err)
	}

	return found, nil
}

// List returns home, then work, then the others by label.
func (r *AddressRepository) List(ctx context.Context, riderID string) ([]address.Address, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+addressColumns+` FROM saved_addresses
		WHERE rider_id = $1
		ORDER BY CASE kind WHEN 'home' THEN 0 WHEN 'work' THEN 1 ELSE 2 END, lower(label), created_at`,
		riderID,
	)
	if err != nil {
		return nil, fmt.Errorf("list saved addresses: %w", err)
	}
	defer rows.Close()

	out := make([]address.Address, 0)

	for rows.Next() {
		found, err := scanAddress(rows)
		if err != nil {
			return nil, fmt.Errorf("scan saved address: %w", err)
		}

		out = append(out, found)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list saved addresses: %w", err)
	}

	return out, nil
}

func (r *AddressRepository) Delete(ctx context.Context, riderID, id string) (address.Address, error) {
	deleted, err := scanAddress(r.pool.QueryRow(ctx,
		`DELETE FROM saved_addresses WHERE id = $1 AND rider_id = $2 RETURNING `+addressColumns, id, riderID))
	if errors.Is(err, pgx.ErrNoRows) {
		return address.Address{}, address.ErrAddressNotFound
	}

	if err != nil {
		return address.Address{}, fmt.Errorf("delete saved address: %w", err)
	}

	return deleted, nil
}

// IdentityOf answers address.Riders from the riders table.
func (r *AddressRepository) IdentityOf(ctx context.Context, riderID string) (string, error) {
	var identityID string

	err := r.pool.QueryRow(ctx, `SELECT identity_id FROM riders WHERE id = $1`, riderID).Scan(&identityID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", address.ErrRiderNotFound
	}

	if err != nil {
		return "", fmt.Errorf("read rider identity: %w", err)
	}

	return identityID, nil
}

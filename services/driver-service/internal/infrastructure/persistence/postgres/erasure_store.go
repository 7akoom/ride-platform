package postgres

import (
	"context"
	"fmt"

	"github.com/7akoom/ride-platform/services/driver-service/internal/application/erasure"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DeletedDriverName replaces the name of a driver whose account was erased.
const DeletedDriverName = "Deleted account"

// ErasureStore erases a deleted account's driver profile (erasure.Eraser).
// When the deletion is asked the driver goes offline. Once it is due the
// name, personal details, documents (with their numbers) and name changes go,
// and the plates are replaced so they are free for someone else; the profile
// and its cars stay, suspended and retired, as what the trips point at. The
// files were deleted from media-service by identity-service.
type ErasureStore struct {
	pool *pgxpool.Pool
}

func NewErasureStore(pool *pgxpool.Pool) *ErasureStore {
	if pool == nil {
		panic("database pool is required")
	}

	return &ErasureStore{pool: pool}
}

// Requested takes the driver offline (a driver on a trip stays busy: a trip
// under way blocks the deletion anyway).
func (s *ErasureStore) Requested(ctx context.Context, account erasure.Account) error {
	if _, err := s.pool.Exec(ctx,
		`UPDATE drivers SET availability_status = 'offline', updated_at = CURRENT_TIMESTAMP
		 WHERE identity_id = $1 AND availability_status = 'available'`, account.IdentityID); err != nil {
		return fmt.Errorf("take a leaving driver offline: %w", err)
	}

	return nil
}

func (s *ErasureStore) Erase(ctx context.Context, account erasure.Account) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin driver erasure: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx,
		`UPDATE drivers
		 SET display_name = $2, status = 'suspended', availability_status = 'offline', rejection_reason = '',
		     vehicle_plate_number = 'DELETED-' || left(replace(id::text, '-', ''), 12),
		     updated_at = CURRENT_TIMESTAMP
		 WHERE identity_id = $1
		 RETURNING id::text`, account.IdentityID, DeletedDriverName)
	if err != nil {
		return fmt.Errorf("erase driver profile: %w", err)
	}

	var ids []string

	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()

			return fmt.Errorf("scan erased driver: %w", err)
		}

		ids = append(ids, id)
	}

	rows.Close()

	if err := rows.Err(); err != nil {
		return fmt.Errorf("erase driver profile: %w", err)
	}

	for _, id := range ids {
		for _, statement := range []string{
			`UPDATE vehicles
			 SET status = 'retired', active = false,
			     plate_number = 'DELETED-' || left(replace(id::text, '-', ''), 12),
			     updated_at = CURRENT_TIMESTAMP
			 WHERE driver_id = $1`,
			`DELETE FROM driver_documents WHERE driver_id = $1`,
			`DELETE FROM driver_details WHERE driver_id = $1`,
			`DELETE FROM driver_name_changes WHERE driver_id = $1`,
		} {
			if _, err := tx.Exec(ctx, statement, id); err != nil {
				return fmt.Errorf("erase driver data: %w", err)
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit driver erasure: %w", err)
	}

	return nil
}

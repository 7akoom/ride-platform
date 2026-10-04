package postgres

import (
	"context"
	"fmt"

	"github.com/7akoom/ride-platform/services/rider-service/internal/application/erasure"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DeletedRiderName replaces the name of a rider whose account was erased.
const DeletedRiderName = "Deleted account"

// ErasureStore erases a deleted account's rider profile (erasure.Eraser):
// the name, the personal details and the saved addresses go; the profile
// row stays, suspended, as the anonymous rider its trips point at. The
// photos were deleted from media-service by identity-service.
type ErasureStore struct {
	pool *pgxpool.Pool
}

func NewErasureStore(pool *pgxpool.Pool) *ErasureStore {
	if pool == nil {
		panic("database pool is required")
	}

	return &ErasureStore{pool: pool}
}

func (s *ErasureStore) Requested(context.Context, erasure.Account) error { return nil }

func (s *ErasureStore) Erase(ctx context.Context, account erasure.Account) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin rider erasure: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx,
		`UPDATE riders SET display_name = $2, status = 'suspended', updated_at = CURRENT_TIMESTAMP
		 WHERE identity_id = $1
		 RETURNING id::text`, account.IdentityID, DeletedRiderName)
	if err != nil {
		return fmt.Errorf("erase rider profile: %w", err)
	}

	var ids []string

	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()

			return fmt.Errorf("scan erased rider: %w", err)
		}

		ids = append(ids, id)
	}

	rows.Close()

	if err := rows.Err(); err != nil {
		return fmt.Errorf("erase rider profile: %w", err)
	}

	for _, id := range ids {
		for _, statement := range []string{
			`DELETE FROM saved_addresses WHERE rider_id = $1`,
			`DELETE FROM rider_details WHERE rider_id = $1`,
		} {
			if _, err := tx.Exec(ctx, statement, id); err != nil {
				return fmt.Errorf("erase rider data: %w", err)
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit rider erasure: %w", err)
	}

	return nil
}

package postgres

import (
	"context"
	"fmt"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/erasure"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErasureStore erases what trips say about a deleted account (erasure.Eraser).
// The trips themselves stay (fares, the company's accounts, disputes) with
// their route and prices; what goes is what describes the person: who rode
// for them and their phone, the pickup details and note, the pickup photo
// (deleted from media-service by identity-service), their rating comments,
// and any share link. A booking still open is cancelled.
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
		return fmt.Errorf("begin trip erasure: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	type statement struct {
		what string
		sql  string
		arg  string
	}

	var statements []statement

	if account.RiderID != "" {
		statements = append(statements,
			statement{"trip shares", `UPDATE trip_shares SET revoked_at = CURRENT_TIMESTAMP
				WHERE revoked_at IS NULL AND trip_id IN (SELECT id FROM trips WHERE rider_id = $1)`, account.RiderID},
			statement{"trips", `UPDATE trips
				SET passenger_name = '', passenger_phone = '', pickup_details = '', pickup_note = '',
				    pickup_photo_media_id = NULL, updated_at = CURRENT_TIMESTAMP
				WHERE rider_id = $1
				  AND (passenger_name <> '' OR pickup_details <> '' OR pickup_note <> '' OR pickup_photo_media_id IS NOT NULL)`, account.RiderID},
			statement{"bookings", `UPDATE scheduled_trips
				SET passenger_name = '', passenger_phone = '',
				    status = CASE WHEN status = 'scheduled' THEN 'cancelled' ELSE status END,
				    cancelled_at = CASE WHEN status = 'scheduled' THEN CURRENT_TIMESTAMP ELSE cancelled_at END
				WHERE rider_id = $1`, account.RiderID},
			statement{"rider's rating comments", `UPDATE trip_ratings SET comment = NULL
				WHERE rater_id = $1 AND comment IS NOT NULL`, account.RiderID},
		)
	}

	if account.DriverID != "" {
		statements = append(statements,
			statement{"driver's rating comments", `UPDATE trip_ratings SET comment = NULL
				WHERE rater_id = $1 AND comment IS NOT NULL`, account.DriverID},
		)
	}

	for _, st := range statements {
		if _, err := tx.Exec(ctx, st.sql, st.arg); err != nil {
			return fmt.Errorf("erase %s: %w", st.what, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit trip erasure: %w", err)
	}

	return nil
}

package postgres

import (
	"context"
	"fmt"

	"github.com/7akoom/ride-platform/services/notification-service/internal/application/erasure"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErasureStore removes a deleted account's devices and inbox
// (erasure.Eraser). Its push tokens go as soon as the deletion is asked: the
// sessions have ended, so nothing should reach the phone any more. The inbox
// (whose texts may name people and amounts) goes once the account is erased.
type ErasureStore struct {
	pool *pgxpool.Pool
}

func NewErasureStore(pool *pgxpool.Pool) *ErasureStore {
	if pool == nil {
		panic("database pool is required")
	}

	return &ErasureStore{pool: pool}
}

func (s *ErasureStore) Requested(ctx context.Context, account erasure.Account) error {
	return s.forEachRecipient(ctx, account, `DELETE FROM devices WHERE recipient_type = $1 AND recipient_id = $2`)
}

func (s *ErasureStore) Erase(ctx context.Context, account erasure.Account) error {
	if err := s.forEachRecipient(ctx, account, `DELETE FROM devices WHERE recipient_type = $1 AND recipient_id = $2`); err != nil {
		return err
	}

	return s.forEachRecipient(ctx, account, `DELETE FROM notifications WHERE recipient_type = $1 AND recipient_id = $2`)
}

func (s *ErasureStore) forEachRecipient(ctx context.Context, account erasure.Account, statement string) error {
	for recipientType, recipientID := range map[string]string{"rider": account.RiderID, "driver": account.DriverID} {
		if recipientID == "" {
			continue
		}

		if _, err := s.pool.Exec(ctx, statement, recipientType, recipientID); err != nil {
			return fmt.Errorf("erase a deleted account's notifications: %w", err)
		}
	}

	return nil
}

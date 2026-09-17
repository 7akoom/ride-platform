package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/topup"
)

type TopUpRepository struct {
	pool *pgxpool.Pool
}

func NewTopUpRepository(pool *pgxpool.Pool) *TopUpRepository {
	if pool == nil {
		panic("postgres pool is required")
	}

	return &TopUpRepository{pool: pool}
}

const topUpSelectSQL = `SELECT id, driver_id, external_reference_id,
	COALESCE(zaincash_transaction_id, ''), amount, status,
	COALESCE(failure_reason, ''), created_at, updated_at
	FROM zaincash_topups`

const topUpReturningSQL = `RETURNING id, driver_id, external_reference_id,
	COALESCE(zaincash_transaction_id, ''), amount, status,
	COALESCE(failure_reason, ''), created_at, updated_at`

func (r *TopUpRepository) Create(ctx context.Context, t topup.TopUp) (topup.TopUp, error) {
	row := r.pool.QueryRow(
		ctx,
		`INSERT INTO zaincash_topups (driver_id, amount, status)
		 VALUES ($1, $2, $3)
		 `+topUpReturningSQL,
		t.DriverID,
		t.Amount,
		string(t.Status),
	)

	created, err := scanTopUp(row)
	if err != nil {
		return topup.TopUp{}, fmt.Errorf("insert top-up: %w", err)
	}

	return created, nil
}

func (r *TopUpRepository) FindByExternalReferenceID(
	ctx context.Context,
	externalReferenceID string,
) (topup.TopUp, error) {
	row := r.pool.QueryRow(ctx, topUpSelectSQL+` WHERE external_reference_id = $1`, externalReferenceID)

	found, err := scanTopUp(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return topup.TopUp{}, topup.ErrTopUpNotFound
		}

		return topup.TopUp{}, fmt.Errorf("select top-up: %w", err)
	}

	return found, nil
}

func (r *TopUpRepository) SetZainCashTransactionID(
	ctx context.Context,
	externalReferenceID string,
	zainCashTransactionID string,
) (topup.TopUp, error) {
	row := r.pool.QueryRow(
		ctx,
		`UPDATE zaincash_topups
		 SET zaincash_transaction_id = $2, updated_at = CURRENT_TIMESTAMP
		 WHERE external_reference_id = $1
		 `+topUpReturningSQL,
		externalReferenceID,
		zainCashTransactionID,
	)

	updated, err := scanTopUp(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return topup.TopUp{}, topup.ErrTopUpNotFound
		}

		return topup.TopUp{}, fmt.Errorf("set zaincash transaction id: %w", err)
	}

	return updated, nil
}

func (r *TopUpRepository) MarkSucceeded(
	ctx context.Context,
	externalReferenceID string,
	zainCashTransactionID string,
) (topup.TopUp, error) {
	row := r.pool.QueryRow(
		ctx,
		`UPDATE zaincash_topups
		 SET status = 'succeeded', zaincash_transaction_id = $2,
		     credited_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		 WHERE external_reference_id = $1
		 `+topUpReturningSQL,
		externalReferenceID,
		zainCashTransactionID,
	)

	updated, err := scanTopUp(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return topup.TopUp{}, topup.ErrTopUpNotFound
		}

		return topup.TopUp{}, fmt.Errorf("mark top-up succeeded: %w", err)
	}

	return updated, nil
}

func (r *TopUpRepository) MarkFailed(
	ctx context.Context,
	externalReferenceID string,
	reason string,
) (topup.TopUp, error) {
	row := r.pool.QueryRow(
		ctx,
		`UPDATE zaincash_topups
		 SET status = 'failed', failure_reason = $2, updated_at = CURRENT_TIMESTAMP
		 WHERE external_reference_id = $1
		 `+topUpReturningSQL,
		externalReferenceID,
		reason,
	)

	updated, err := scanTopUp(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return topup.TopUp{}, topup.ErrTopUpNotFound
		}

		return topup.TopUp{}, fmt.Errorf("mark top-up failed: %w", err)
	}

	return updated, nil
}

func scanTopUp(row pgx.Row) (topup.TopUp, error) {
	var t topup.TopUp
	var status string

	err := row.Scan(
		&t.ID,
		&t.DriverID,
		&t.ExternalReferenceID,
		&t.ZainCashTransactionID,
		&t.Amount,
		&status,
		&t.FailureReason,
		&t.CreatedAt,
		&t.UpdatedAt,
	)
	if err != nil {
		return topup.TopUp{}, err
	}

	t.Status = topup.Status(status)

	return t, nil
}

// Compile-time proof that this repository satisfies the port.
var _ topup.Repository = (*TopUpRepository)(nil)

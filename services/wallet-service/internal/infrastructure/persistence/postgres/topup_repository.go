package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/topup"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

// TopUpRepository keeps payment attempts with any provider. The balance
// itself only ever moves through wallet.Service.TopUp.
type TopUpRepository struct {
	pool *pgxpool.Pool
}

func NewTopUpRepository(pool *pgxpool.Pool) *TopUpRepository {
	if pool == nil {
		panic("postgres pool is required")
	}

	return &TopUpRepository{pool: pool}
}

const topUpColumns = `id, owner_type, owner_id, provider, external_reference_id,
	COALESCE(provider_transaction_id, ''), amount, currency_code, status,
	COALESCE(failure_reason, ''), created_at, updated_at`

func (r *TopUpRepository) one(ctx context.Context, what, query string, args ...any) (topup.TopUp, error) {
	found, err := scanTopUp(r.pool.QueryRow(ctx, query, args...))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return topup.TopUp{}, topup.ErrTopUpNotFound
		}

		return topup.TopUp{}, fmt.Errorf("%s: %w", what, err)
	}

	return found, nil
}

func (r *TopUpRepository) Create(ctx context.Context, t topup.TopUp) (topup.TopUp, error) {
	return r.one(
		ctx, "insert top-up",
		`INSERT INTO provider_topups (owner_type, owner_id, provider, amount, currency_code, status)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING `+topUpColumns,
		string(t.OwnerType), t.OwnerID, t.Provider, t.Amount, t.CurrencyCode, string(t.Status),
	)
}

func (r *TopUpRepository) FindByExternalReferenceID(ctx context.Context, externalReferenceID string) (topup.TopUp, error) {
	return r.one(
		ctx, "select top-up",
		`SELECT `+topUpColumns+` FROM provider_topups WHERE external_reference_id::text = $1`,
		externalReferenceID,
	)
}

func (r *TopUpRepository) FindForOwner(ctx context.Context, ownerType wallet.OwnerType, ownerID, id string) (topup.TopUp, error) {
	return r.one(
		ctx, "select top-up",
		`SELECT `+topUpColumns+` FROM provider_topups WHERE id = $1 AND owner_type = $2 AND owner_id = $3`,
		id, string(ownerType), ownerID,
	)
}

func (r *TopUpRepository) SetProviderTransactionID(ctx context.Context, externalReferenceID, providerTransactionID string) (topup.TopUp, error) {
	return r.one(
		ctx, "set the provider's transaction id",
		`UPDATE provider_topups
		 SET provider_transaction_id = $2, updated_at = CURRENT_TIMESTAMP
		 WHERE external_reference_id::text = $1
		 RETURNING `+topUpColumns,
		externalReferenceID, providerTransactionID,
	)
}

func (r *TopUpRepository) MarkSucceeded(ctx context.Context, externalReferenceID string) (topup.TopUp, error) {
	return r.one(
		ctx, "mark top-up succeeded",
		`UPDATE provider_topups
		 SET status = 'succeeded', credited_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		 WHERE external_reference_id::text = $1
		 RETURNING `+topUpColumns,
		externalReferenceID,
	)
}

func (r *TopUpRepository) MarkFailed(ctx context.Context, externalReferenceID, reason string) (topup.TopUp, error) {
	return r.one(
		ctx, "mark top-up failed",
		`UPDATE provider_topups
		 SET status = 'failed', failure_reason = $2, updated_at = CURRENT_TIMESTAMP
		 WHERE external_reference_id::text = $1 AND status <> 'succeeded'
		 RETURNING `+topUpColumns,
		externalReferenceID, reason,
	)
}

func scanTopUp(row pgx.Row) (topup.TopUp, error) {
	var (
		t                 topup.TopUp
		ownerType, status string
	)

	err := row.Scan(
		&t.ID, &ownerType, &t.OwnerID, &t.Provider, &t.ExternalReferenceID,
		&t.ProviderTransactionID, &t.Amount, &t.CurrencyCode, &status,
		&t.FailureReason, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return topup.TopUp{}, err
	}

	t.OwnerType = wallet.OwnerType(ownerType)
	t.Status = topup.Status(status)

	return t, nil
}

// Compile-time proof that this repository satisfies the port.
var _ topup.Repository = (*TopUpRepository)(nil)

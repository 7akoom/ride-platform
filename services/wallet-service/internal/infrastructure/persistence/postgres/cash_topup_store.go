package postgres

import (
	"context"
	"fmt"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/operations"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

var _ operations.CashTopUpStore = (*OperationsStore)(nil)

// CashTopUp credits the wallet with the cash, records the receipt, and tells the
// owner (wallet.topped_up, provider "office"), all in one transaction.
func (s *OperationsStore) CashTopUp(ctx context.Context, record operations.CashTopUpRecord) (operations.Adjustment, wallet.Wallet, error) {
	config, err := s.wallets.GetActiveConfig(ctx)
	if err != nil {
		return operations.Adjustment{}, wallet.Wallet{}, err
	}

	tx, err := s.wallets.pool.Begin(ctx)
	if err != nil {
		return operations.Adjustment{}, wallet.Wallet{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	target, err := ensureWalletTx(ctx, tx, record.OwnerType, record.OwnerID, config.CurrencyCode)
	if err != nil {
		return operations.Adjustment{}, wallet.Wallet{}, err
	}

	updated, transaction, err := applyMovementTx(ctx, tx, target, movementFor(
		config, record.OwnerType, record.OwnerID, wallet.TxCashTopUp, record.Amount, "",
		"Cash top-up, receipt "+record.ReceiptNumber,
	))
	if err != nil {
		return operations.Adjustment{}, wallet.Wallet{}, err
	}

	made, err := scanAdjustment(tx.QueryRow(
		ctx,
		`INSERT INTO wallet_adjustments
		    (kind, owner_type, owner_id, currency_code, amount, reason, transaction_id, created_by,
		     idempotency_key, receipt_number)
		 VALUES ('cash_top_up', $1, $2, $3, $4, $5, $6, $7, $8, $9)
		 RETURNING `+adjustmentColumns,
		string(record.OwnerType), record.OwnerID, target.CurrencyCode, record.Amount,
		record.Note, transaction.ID, record.CreatedBy, record.IdempotencyKey, record.ReceiptNumber,
	))
	if err != nil {
		switch {
		case isUniqueViolation(err, "wallet_adjustments_creator_key_unique"):
			return operations.Adjustment{}, wallet.Wallet{}, wallet.ErrDuplicateRequest
		case isUniqueViolation(err, "wallet_adjustments_receipt_unique"):
			return operations.Adjustment{}, wallet.Wallet{}, operations.ErrReceiptUsed
		}

		return operations.Adjustment{}, wallet.Wallet{}, fmt.Errorf("insert cash top-up: %w", err)
	}

	if err := insertOutboxEvent(ctx, tx, "cash_topup", made.ID, EventToppedUp, map[string]any{
		"cash_topup_id": made.ID,
		"owner_type":    string(record.OwnerType),
		"owner_id":      record.OwnerID,
		"provider":      "office",
		"amount":        record.Amount.String(),
		"balance":       updated.Balance.String(),
		"currency_code": updated.CurrencyCode,
	}); err != nil {
		return operations.Adjustment{}, wallet.Wallet{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return operations.Adjustment{}, wallet.Wallet{}, fmt.Errorf("commit transaction: %w", err)
	}

	return made, updated, nil
}

// ListCashTopUps reads the cash in the range, newest first, with the count and
// total of all of it.
func (s *OperationsStore) ListCashTopUps(ctx context.Context, query operations.CashTopUpQuery) (operations.CashTopUps, error) {
	const where = `kind = 'cash_top_up' AND created_at >= $1 AND created_at < $2 AND ($3 = '' OR created_by::text = $3)`

	out := operations.CashTopUps{}

	var currency *string

	if err := s.wallets.pool.QueryRow(ctx,
		`SELECT count(*), COALESCE(sum(amount), 0), max(currency_code) FROM wallet_adjustments WHERE `+where,
		query.From, query.To, query.CreatedBy,
	).Scan(&out.Count, &out.Total, &currency); err != nil {
		return operations.CashTopUps{}, fmt.Errorf("total cash top-ups: %w", err)
	}

	if currency != nil {
		out.CurrencyCode = *currency
	}

	rows, err := s.wallets.pool.Query(ctx,
		`SELECT `+adjustmentColumns+` FROM wallet_adjustments WHERE `+where+` ORDER BY created_at DESC, id LIMIT $4`,
		query.From, query.To, query.CreatedBy, query.Limit,
	)
	if err != nil {
		return operations.CashTopUps{}, fmt.Errorf("list cash top-ups: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		a, err := scanAdjustment(rows)
		if err != nil {
			return operations.CashTopUps{}, fmt.Errorf("read cash top-up: %w", err)
		}

		out.TopUps = append(out.TopUps, a)
	}

	return out, rows.Err()
}

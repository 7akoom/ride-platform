package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

var _ wallet.StatementReader = (*WalletRepository)(nil)

// statementFilter is the WHERE of a statement's rows ($1 wallet, $2 from,
// $3 to, $4 direction, $5 types).
const statementFilter = `t.wallet_id = $1 AND t.created_at >= $2 AND t.created_at < $3
		   AND ($4 = '' OR ($4 = 'in' AND t.amount > 0) OR ($4 = 'out' AND t.amount < 0))
		   AND (cardinality($5::text[]) = 0 OR t.type = ANY($5::text[]))`

func (r *WalletRepository) Statement(ctx context.Context, query wallet.StatementQuery) (wallet.Statement, error) {
	config, err := r.GetActiveConfig(ctx)
	if err != nil {
		return wallet.Statement{}, err
	}

	statement := wallet.Statement{
		CurrencyCode: config.CurrencyCode,
		Opening:      decimal.Zero,
		Closing:      decimal.Zero,
		TotalIn:      decimal.Zero,
		TotalOut:     decimal.Zero,
	}

	found, err := r.FindWallet(ctx, query.OwnerType, query.OwnerID)

	switch {
	case errors.Is(err, wallet.ErrWalletNotFound):
		// No wallet yet: an empty statement.
		return statement, nil
	case err != nil:
		return wallet.Statement{}, err
	}

	statement.CurrencyCode = found.CurrencyCode

	types := make([]string, 0, len(query.Types))
	for _, t := range query.Types {
		types = append(types, string(t))
	}

	balanceAt := func(before any) (decimal.Decimal, error) {
		var balance decimal.Decimal

		err := r.pool.QueryRow(
			ctx,
			`SELECT balance_after FROM wallet_transactions
			 WHERE wallet_id = $1 AND created_at < $2
			 ORDER BY created_at DESC, seq DESC
			 LIMIT 1`,
			found.ID, before,
		).Scan(&balance)
		if errors.Is(err, pgx.ErrNoRows) {
			return decimal.Zero, nil
		}

		return balance, err
	}

	if statement.Opening, err = balanceAt(query.From); err != nil {
		return wallet.Statement{}, fmt.Errorf("read the opening balance: %w", err)
	}

	if statement.Closing, err = balanceAt(query.To); err != nil {
		return wallet.Statement{}, fmt.Errorf("read the closing balance: %w", err)
	}

	args := []any{found.ID, query.From, query.To, query.Direction, types}

	if err := r.pool.QueryRow(
		ctx,
		`SELECT COALESCE(SUM(t.amount) FILTER (WHERE t.amount > 0), 0),
		        COALESCE(-SUM(t.amount) FILTER (WHERE t.amount < 0), 0)
		 FROM wallet_transactions t
		 WHERE `+statementFilter,
		args...,
	).Scan(&statement.TotalIn, &statement.TotalOut); err != nil {
		return wallet.Statement{}, fmt.Errorf("sum the statement: %w", err)
	}

	rows, err := r.pool.Query(
		ctx,
		`SELECT t.id, t.wallet_id, t.type, t.amount, t.balance_after,
		        COALESCE(t.trip_id::text, ''), COALESCE(t.transfer_id::text, ''),
		        COALESCE(t.description, ''), t.created_at
		 FROM wallet_transactions t
		 WHERE `+statementFilter+`
		 ORDER BY t.created_at DESC, t.seq DESC
		 OFFSET $6 LIMIT $7`,
		append(args, query.Offset, query.Limit)...,
	)
	if err != nil {
		return wallet.Statement{}, fmt.Errorf("select statement rows: %w", err)
	}

	statement.Entries, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (wallet.Transaction, error) {
		var (
			t        wallet.Transaction
			typeName string
		)

		err := row.Scan(&t.ID, &t.WalletID, &typeName, &t.Amount, &t.BalanceAfter, &t.TripID, &t.TransferID, &t.Description, &t.CreatedAt)
		t.Type = wallet.TransactionType(typeName)

		return t, err
	})
	if err != nil {
		return wallet.Statement{}, fmt.Errorf("read statement rows: %w", err)
	}

	return statement, nil
}

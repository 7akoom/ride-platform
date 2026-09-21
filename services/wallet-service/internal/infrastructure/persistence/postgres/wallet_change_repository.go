package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

// CreditTripChange records the change and credits the rider's wallet in ONE transaction.
// The trip_change_credits row goes in first: its UNIQUE trip_id is what makes the credit
// happen at most once, however many times the driver's app retries.
func (r *WalletRepository) CreditTripChange(
	ctx context.Context,
	input wallet.ChangeCreditInput,
) (wallet.ChangeCredit, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return wallet.ChangeCredit{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var createdAt time.Time

	err = tx.QueryRow(
		ctx,
		`INSERT INTO trip_change_credits
            (trip_id, rider_id, driver_id, currency_code, cash_due, cash_received, change_amount)
         VALUES ($1, $2, $3, $4, $5, $6, $7)
         RETURNING created_at`,
		input.TripID,
		input.RiderID,
		input.DriverID,
		input.CurrencyCode,
		input.CashDue,
		input.CashReceived,
		input.ChangeAmount,
	).Scan(&createdAt)
	if err != nil {
		var pgErr *pgconn.PgError

		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode {
			// Already recorded. The aborted transaction is dropped before reading.
			_ = tx.Rollback(ctx)

			return r.existingChangeCredit(ctx, input)
		}

		return wallet.ChangeCredit{}, fmt.Errorf("insert trip change credit: %w", err)
	}

	riderWallet, err := ensureWalletTx(ctx, tx, wallet.OwnerRider, input.RiderID, input.CurrencyCode)
	if err != nil {
		return wallet.ChangeCredit{}, err
	}

	riderWallet, _, err = applyMovementTx(ctx, tx, riderWallet, wallet.MovementInput{
		Type:           wallet.TxChangeCredit,
		Amount:         input.ChangeAmount,
		TripID:         input.TripID,
		IdempotencyKey: "change-credit:" + input.TripID,
		Description:    "Change the driver could not return in cash",
	})
	if err != nil {
		return wallet.ChangeCredit{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return wallet.ChangeCredit{}, fmt.Errorf("commit transaction: %w", err)
	}

	return wallet.ChangeCredit{
		TripID:       input.TripID,
		RiderID:      input.RiderID,
		DriverID:     input.DriverID,
		CurrencyCode: input.CurrencyCode,
		CashDue:      input.CashDue,
		CashReceived: input.CashReceived,
		ChangeAmount: input.ChangeAmount,
		RiderBalance: riderWallet.Balance,
		CreatedAt:    createdAt,
	}, nil
}

// existingChangeCredit answers a repeated request: the same amount is a harmless repeat,
// a different one is refused so the first record cannot be overwritten.
func (r *WalletRepository) existingChangeCredit(
	ctx context.Context,
	input wallet.ChangeCreditInput,
) (wallet.ChangeCredit, error) {
	var existing wallet.ChangeCredit

	err := r.pool.QueryRow(
		ctx,
		`SELECT c.trip_id, c.rider_id, c.driver_id, c.currency_code,
                c.cash_due, c.cash_received, c.change_amount, c.created_at,
                COALESCE(w.balance, 0)
         FROM trip_change_credits c
         LEFT JOIN wallets w ON w.owner_type = 'rider' AND w.owner_id = c.rider_id
         WHERE c.trip_id = $1`,
		input.TripID,
	).Scan(
		&existing.TripID,
		&existing.RiderID,
		&existing.DriverID,
		&existing.CurrencyCode,
		&existing.CashDue,
		&existing.CashReceived,
		&existing.ChangeAmount,
		&existing.CreatedAt,
		&existing.RiderBalance,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Removed between the two statements: nothing to compare with.
			return wallet.ChangeCredit{}, wallet.ErrChangeAlreadyRecorded
		}

		return wallet.ChangeCredit{}, fmt.Errorf("select trip change credit: %w", err)
	}

	if !existing.CashReceived.Equal(input.CashReceived) {
		return wallet.ChangeCredit{}, wallet.ErrChangeAlreadyRecorded
	}

	existing.Repeated = true

	return existing, nil
}

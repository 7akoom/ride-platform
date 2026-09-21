package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

const uniqueViolationCode = "23505"
const checkViolationCode = "23514"

const schemaVersion = 1

type WalletRepository struct {
	pool *pgxpool.Pool
}

func NewWalletRepository(pool *pgxpool.Pool) *WalletRepository {
	if pool == nil {
		panic("postgres pool is required")
	}

	return &WalletRepository{pool: pool}
}

func (r *WalletRepository) GetActiveConfig(
	ctx context.Context,
) (wallet.Config, error) {
	row := r.pool.QueryRow(
		ctx,
		`SELECT id, currency_code, commission_rate, suspension_threshold,
		        minimum_payout_amount, created_at
		 FROM wallet_configs
		 ORDER BY created_at DESC
		 LIMIT 1`,
	)

	var config wallet.Config

	err := row.Scan(
		&config.ID,
		&config.CurrencyCode,
		&config.CommissionRate,
		&config.SuspensionThreshold,
		&config.MinimumPayoutAmount,
		&config.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return wallet.Config{}, wallet.ErrNoActiveConfig
		}

		return wallet.Config{}, fmt.Errorf("select active wallet config: %w", err)
	}

	return config, nil
}

func (r *WalletRepository) FindWallet(
	ctx context.Context,
	ownerType wallet.OwnerType,
	ownerID string,
) (wallet.Wallet, error) {
	row := r.pool.QueryRow(
		ctx,
		walletSelectSQL+` WHERE owner_type = $1 AND owner_id = $2`,
		string(ownerType),
		ownerID,
	)

	found, err := scanWallet(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return wallet.Wallet{}, wallet.ErrWalletNotFound
		}

		return wallet.Wallet{}, fmt.Errorf("select wallet: %w", err)
	}

	return found, nil
}

// FindOrCreateWallet uses an upsert so two concurrent first-time
// requests for the same owner can't both insert.
func (r *WalletRepository) FindOrCreateWallet(
	ctx context.Context,
	ownerType wallet.OwnerType,
	ownerID string,
	currencyCode string,
) (wallet.Wallet, error) {
	row := r.pool.QueryRow(
		ctx,
		`INSERT INTO wallets (owner_type, owner_id, currency_code)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (owner_type, owner_id) DO UPDATE
		 SET updated_at = wallets.updated_at
		 RETURNING id, owner_type, owner_id, currency_code, balance,
		           blocked, created_at, updated_at`,
		string(ownerType),
		ownerID,
		currencyCode,
	)

	found, err := scanWallet(row)
	if err != nil {
		return wallet.Wallet{}, fmt.Errorf("find or create wallet: %w", err)
	}

	return found, nil
}

func (r *WalletRepository) ApplyMovement(
	ctx context.Context,
	input wallet.MovementInput,
) (wallet.Wallet, wallet.Transaction, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return wallet.Wallet{}, wallet.Transaction{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	config, err := r.GetActiveConfig(ctx)
	if err != nil {
		return wallet.Wallet{}, wallet.Transaction{}, err
	}

	target, err := ensureWalletTx(ctx, tx, input.OwnerType, input.OwnerID, config.CurrencyCode)
	if err != nil {
		return wallet.Wallet{}, wallet.Transaction{}, err
	}

	// A blocked wallet still accepts incoming money — that is exactly
	// how a suspended driver reactivates. Only outgoing movements are
	// refused.
	if target.Blocked && input.Amount.IsNegative() {
		return wallet.Wallet{}, wallet.Transaction{}, wallet.ErrWalletBlocked
	}

	updated, transaction, err := applyMovementTx(ctx, tx, target, input)
	if err != nil {
		return wallet.Wallet{}, wallet.Transaction{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return wallet.Wallet{}, wallet.Transaction{}, fmt.Errorf("commit transaction: %w", err)
	}

	return updated, transaction, nil
}

func (r *WalletRepository) FindSettlement(
	ctx context.Context,
	tripID string,
) (wallet.Settlement, bool, error) {
	row := r.pool.QueryRow(
		ctx,
		`SELECT s.trip_id, s.rider_id, s.driver_id, s.currency_code,
		        s.payment_method, s.fare_amount, s.commission_rate,
		        s.commission_amount, s.driver_earning,
		        s.wallet_amount, s.cash_amount,
		        COALESCE(rw.balance, 0), COALESCE(dw.balance, 0)
		 FROM trip_settlements s
		 LEFT JOIN wallets rw ON rw.owner_type = 'rider' AND rw.owner_id = s.rider_id
		 LEFT JOIN wallets dw ON dw.owner_type = 'driver' AND dw.owner_id = s.driver_id
		 WHERE s.trip_id = $1`,
		tripID,
	)

	var settlement wallet.Settlement
	var paymentMethod string

	err := row.Scan(
		&settlement.TripID,
		&settlement.RiderID,
		&settlement.DriverID,
		&settlement.CurrencyCode,
		&paymentMethod,
		&settlement.FareAmount,
		&settlement.CommissionRate,
		&settlement.CommissionAmount,
		&settlement.DriverEarning,
		&settlement.WalletAmount,
		&settlement.CashAmount,
		&settlement.RiderBalance,
		&settlement.DriverBalance,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return wallet.Settlement{}, false, nil
		}

		return wallet.Settlement{}, false, fmt.Errorf("select settlement: %w", err)
	}

	settlement.PaymentMethod = wallet.PaymentMethod(paymentMethod)

	return settlement, true, nil
}

// SettleTrip moves every part of a trip's money in one transaction. The
// direction of the driver's movement depends on the payment method — see
// the service layer's SettleTrip for the full reasoning.
func (r *WalletRepository) SettleTrip(
	ctx context.Context,
	input wallet.SettleInput,
) (wallet.Settlement, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return wallet.Settlement{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	riderWallet, err := ensureWalletTx(ctx, tx, wallet.OwnerRider, input.RiderID, input.CurrencyCode)
	if err != nil {
		return wallet.Settlement{}, err
	}

	driverWallet, err := ensureWalletTx(ctx, tx, wallet.OwnerDriver, input.DriverID, input.CurrencyCode)
	if err != nil {
		return wallet.Settlement{}, err
	}

	// walletAmount is what moved through wallets, cashAmount is what the rider
	// handed to the driver. Both go into the trip.settled event.
	zero := input.FareAmount.Sub(input.FareAmount)
	walletAmount := zero
	cashAmount := zero

	switch input.PaymentMethod {
	case wallet.PaymentWallet:
		// The rider's wallet pays what it holds and the rest is cash in the
		// driver's hand, so a wallet trip can never fail for lack of funds.
		// The rider row is locked above, so the balance read here is the one
		// the debit is applied to.
		walletAmount, cashAmount = wallet.SplitWalletPayment(input.FareAmount, riderWallet.Balance)

		if walletAmount.IsPositive() {
			riderDescription := "Trip fare"
			if cashAmount.IsPositive() {
				riderDescription = "Trip fare (part paid from the wallet, the rest in cash)"
			}

			riderWallet, _, err = applyMovementTx(ctx, tx, riderWallet, wallet.MovementInput{
				Type:        wallet.TxTripPayment,
				Amount:      walletAmount.Neg(),
				TripID:      input.TripID,
				Description: riderDescription,
			})
			if err != nil {
				return wallet.Settlement{}, err
			}
		}

		// The platform holds the wallet part, the driver holds the cash part,
		// and the driver is entitled to the fare minus the commission: so the
		// platform owes the driver (wallet part - commission). A credit when
		// the wallet part is larger, a debit when it is smaller.
		movement := wallet.DriverSettlementAmount(walletAmount, input.CommissionAmount)

		switch {
		case movement.IsPositive():
			earningDescription := "Trip earning (after commission)"
			if cashAmount.IsPositive() {
				earningDescription = "Trip earning (wallet part, after commission)"
			}

			driverWallet, _, err = applyMovementTx(ctx, tx, driverWallet, wallet.MovementInput{
				Type:            wallet.TxTripEarning,
				Amount:          movement,
				TripID:          input.TripID,
				Description:     earningDescription,
				SuspensionFloor: &input.SuspensionFloor,
			})
			if err != nil {
				return wallet.Settlement{}, err
			}

		case movement.IsNegative():
			commissionDescription := "Platform commission on cash trip"
			if walletAmount.IsPositive() {
				commissionDescription = "Platform commission on a trip paid partly in cash"
			}

			driverWallet, _, err = applyMovementTx(ctx, tx, driverWallet, wallet.MovementInput{
				Type:            wallet.TxCommission,
				Amount:          movement,
				TripID:          input.TripID,
				Description:     commissionDescription,
				AllowNegative:   true,
				SuspensionFloor: &input.SuspensionFloor,
			})
			if err != nil {
				return wallet.Settlement{}, err
			}
		}

	case wallet.PaymentCard:
		// The card processor collects the fare; the platform keeps its
		// commission and credits the driver the rest. The rider's
		// wallet is untouched.
		driverWallet, _, err = applyMovementTx(ctx, tx, driverWallet, wallet.MovementInput{
			Type:            wallet.TxTripEarning,
			Amount:          input.DriverEarning,
			TripID:          input.TripID,
			Description:     "Trip earning (after commission)",
			SuspensionFloor: &input.SuspensionFloor,
		})
		if err != nil {
			return wallet.Settlement{}, err
		}

	case wallet.PaymentCash:
		// The driver already holds the entire fare, including the
		// platform's commission — so the commission is debited from
		// their digital balance, which is allowed to go negative.
		cashAmount = input.FareAmount

		driverWallet, _, err = applyMovementTx(ctx, tx, driverWallet, wallet.MovementInput{
			Type:            wallet.TxCommission,
			Amount:          input.CommissionAmount.Neg(),
			TripID:          input.TripID,
			Description:     "Platform commission on cash trip",
			AllowNegative:   true,
			SuspensionFloor: &input.SuspensionFloor,
		})
		if err != nil {
			return wallet.Settlement{}, err
		}
	}

	if _, err := tx.Exec(
		ctx,
		`INSERT INTO trip_settlements
		    (trip_id, rider_id, driver_id, currency_code, payment_method,
		     fare_amount, commission_rate, commission_amount, driver_earning,
		     wallet_amount, cash_amount)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		input.TripID,
		input.RiderID,
		input.DriverID,
		input.CurrencyCode,
		string(input.PaymentMethod),
		input.FareAmount,
		input.CommissionRate,
		input.CommissionAmount,
		input.DriverEarning,
		walletAmount,
		cashAmount,
	); err != nil {
		var pgErr *pgconn.PgError

		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode {
			// A concurrent settlement won. Its result is equally valid.
			return wallet.Settlement{}, wallet.ErrDuplicateRequest
		}

		return wallet.Settlement{}, fmt.Errorf("insert trip settlement: %w", err)
	}

	payload, err := json.Marshal(map[string]string{
		"trip_id":           input.TripID,
		"rider_id":          input.RiderID,
		"driver_id":         input.DriverID,
		"payment_method":    string(input.PaymentMethod),
		"fare_amount":       input.FareAmount.String(),
		"commission_amount": input.CommissionAmount.String(),
		"driver_earning":    input.DriverEarning.String(),
		"wallet_amount":     walletAmount.String(),
		"cash_amount":       cashAmount.String(),
	})
	if err != nil {
		return wallet.Settlement{}, fmt.Errorf("marshal trip.settled payload: %w", err)
	}

	if _, err := tx.Exec(
		ctx,
		`INSERT INTO outbox_events
		    (aggregate_type, aggregate_id, event_type, schema_version,
		     payload, occurred_at, available_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $6)`,
		"settlement",
		input.TripID,
		"trip.settled",
		schemaVersion,
		payload,
		time.Now().UTC(),
	); err != nil {
		return wallet.Settlement{}, fmt.Errorf("insert trip.settled outbox event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return wallet.Settlement{}, fmt.Errorf("commit transaction: %w", err)
	}

	return wallet.Settlement{
		TripID:           input.TripID,
		RiderID:          input.RiderID,
		DriverID:         input.DriverID,
		CurrencyCode:     input.CurrencyCode,
		PaymentMethod:    input.PaymentMethod,
		FareAmount:       input.FareAmount,
		CommissionRate:   input.CommissionRate,
		CommissionAmount: input.CommissionAmount,
		DriverEarning:    input.DriverEarning,
		WalletAmount:     walletAmount,
		CashAmount:       cashAmount,
		RiderBalance:     riderWallet.Balance,
		DriverBalance:    driverWallet.Balance,
	}, nil
}

func (r *WalletRepository) ListTransactions(
	ctx context.Context,
	ownerType wallet.OwnerType,
	ownerID string,
	limit int,
) ([]wallet.Transaction, error) {
	rows, err := r.pool.Query(
		ctx,
		`SELECT t.id, t.wallet_id, t.type, t.amount, t.balance_after,
		        COALESCE(t.trip_id::text, ''), COALESCE(t.description, ''),
		        t.created_at
		 FROM wallet_transactions t
		 JOIN wallets w ON w.id = t.wallet_id
		 WHERE w.owner_type = $1 AND w.owner_id = $2
		 ORDER BY t.created_at DESC
		 LIMIT $3`,
		string(ownerType),
		ownerID,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("select transactions: %w", err)
	}
	defer rows.Close()

	var transactions []wallet.Transaction

	for rows.Next() {
		var transaction wallet.Transaction
		var transactionType string

		if err := rows.Scan(
			&transaction.ID,
			&transaction.WalletID,
			&transactionType,
			&transaction.Amount,
			&transaction.BalanceAfter,
			&transaction.TripID,
			&transaction.Description,
			&transaction.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan transaction: %w", err)
		}

		transaction.Type = wallet.TransactionType(transactionType)
		transactions = append(transactions, transaction)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate transactions: %w", err)
	}

	return transactions, nil
}

const walletSelectSQL = `SELECT id, owner_type, owner_id, currency_code, balance,
                                blocked, created_at, updated_at
                         FROM wallets`

// ensureWalletTx gets the wallet and locks its row FOR UPDATE, so
// concurrent movements against the same wallet serialize instead of
// racing on the balance. This is the single most important line in the
// service for correctness under load.
func ensureWalletTx(
	ctx context.Context,
	tx pgx.Tx,
	ownerType wallet.OwnerType,
	ownerID string,
	currencyCode string,
) (wallet.Wallet, error) {
	if _, err := tx.Exec(
		ctx,
		`INSERT INTO wallets (owner_type, owner_id, currency_code)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (owner_type, owner_id) DO NOTHING`,
		string(ownerType),
		ownerID,
		currencyCode,
	); err != nil {
		return wallet.Wallet{}, fmt.Errorf("ensure wallet exists: %w", err)
	}

	row := tx.QueryRow(
		ctx,
		walletSelectSQL+` WHERE owner_type = $1 AND owner_id = $2 FOR UPDATE`,
		string(ownerType),
		ownerID,
	)

	locked, err := scanWallet(row)
	if err != nil {
		return wallet.Wallet{}, fmt.Errorf("lock wallet: %w", err)
	}

	return locked, nil
}

// applyMovementTx assumes the wallet row is already locked by the caller.
func applyMovementTx(
	ctx context.Context,
	tx pgx.Tx,
	target wallet.Wallet,
	input wallet.MovementInput,
) (wallet.Wallet, wallet.Transaction, error) {
	newBalance := target.Balance.Add(input.Amount)

	// Only a debit can run out of funds. A credit (a driver's deposit
	// against an already-negative prepaid balance, say) never makes the
	// balance worse, so it must not be refused just because the balance
	// is still below zero afterwards.
	if !input.AllowNegative && input.Amount.IsNegative() && newBalance.IsNegative() {
		return wallet.Wallet{}, wallet.Transaction{}, wallet.ErrInsufficientFunds
	}

	// Suspension is recomputed from the new balance in the same UPDATE,
	// so the blocked flag can never drift out of sync with the money.
	// A driver crossing below the floor is suspended immediately; a
	// deposit that lifts them back above it reinstates them in the same
	// transaction as the deposit.
	blocked := target.Blocked

	if input.SuspensionFloor != nil {
		blocked = newBalance.LessThanOrEqual(*input.SuspensionFloor)
	}

	var updated wallet.Wallet

	row := tx.QueryRow(
		ctx,
		`UPDATE wallets
		 SET balance = $2,
		     blocked = $3,
		     updated_at = CURRENT_TIMESTAMP
		 WHERE id = $1
		 RETURNING id, owner_type, owner_id, currency_code, balance,
		           blocked, created_at, updated_at`,
		target.ID,
		newBalance,
		blocked,
	)

	updated, err := scanWallet(row)
	if err != nil {
		var pgErr *pgconn.PgError

		// The rider-non-negative CHECK constraint is a second line of
		// defence behind the Go-side check above.
		if errors.As(err, &pgErr) && pgErr.Code == checkViolationCode {
			return wallet.Wallet{}, wallet.Transaction{}, wallet.ErrInsufficientFunds
		}

		return wallet.Wallet{}, wallet.Transaction{}, fmt.Errorf("update wallet balance: %w", err)
	}

	var idempotencyKey, tripID, description *string

	if input.IdempotencyKey != "" {
		idempotencyKey = &input.IdempotencyKey
	}

	if input.TripID != "" {
		tripID = &input.TripID
	}

	if input.Description != "" {
		description = &input.Description
	}

	var transaction wallet.Transaction
	var transactionType string

	txRow := tx.QueryRow(
		ctx,
		`INSERT INTO wallet_transactions
		    (wallet_id, type, amount, balance_after, trip_id,
		     idempotency_key, description)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id, wallet_id, type, amount, balance_after,
		           COALESCE(trip_id::text, ''), COALESCE(description, ''),
		           created_at`,
		target.ID,
		string(input.Type),
		input.Amount,
		newBalance,
		tripID,
		idempotencyKey,
		description,
	)

	if err := txRow.Scan(
		&transaction.ID,
		&transaction.WalletID,
		&transactionType,
		&transaction.Amount,
		&transaction.BalanceAfter,
		&transaction.TripID,
		&transaction.Description,
		&transaction.CreatedAt,
	); err != nil {
		var pgErr *pgconn.PgError

		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode {
			return wallet.Wallet{}, wallet.Transaction{}, wallet.ErrDuplicateRequest
		}

		return wallet.Wallet{}, wallet.Transaction{}, fmt.Errorf("insert wallet transaction: %w", err)
	}

	transaction.Type = wallet.TransactionType(transactionType)

	return updated, transaction, nil
}

func scanWallet(row pgx.Row) (wallet.Wallet, error) {
	var found wallet.Wallet
	var ownerType string

	err := row.Scan(
		&found.ID,
		&ownerType,
		&found.OwnerID,
		&found.CurrencyCode,
		&found.Balance,
		&found.Blocked,
		&found.CreatedAt,
		&found.UpdatedAt,
	)
	if err != nil {
		return wallet.Wallet{}, err
	}

	found.OwnerType = wallet.OwnerType(ownerType)

	return found, nil
}

// Compile-time proof that this repository satisfies the port. Catches
// signature drift at build time rather than at wiring time.
var _ wallet.Repository = (*WalletRepository)(nil)

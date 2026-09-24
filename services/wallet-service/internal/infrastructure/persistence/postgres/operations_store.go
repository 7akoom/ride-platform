package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/operations"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

// OperationsStore writes staff money operations and drivers' payouts, with
// the same ledger and locking as every other movement.
type OperationsStore struct {
	wallets *WalletRepository
}

var _ operations.Store = (*OperationsStore)(nil)

func NewOperationsStore(wallets *WalletRepository) *OperationsStore {
	if wallets == nil {
		panic("wallet repository is required")
	}

	return &OperationsStore{wallets: wallets}
}

func (s *OperationsStore) Config(ctx context.Context) (wallet.Config, error) {
	return s.wallets.GetActiveConfig(ctx)
}

const adjustmentColumns = `id, kind, owner_type, owner_id, currency_code, amount,
        COALESCE(trip_id::text, ''), COALESCE(driver_id::text, ''), driver_amount,
        reason, transaction_id, created_by, created_at`

func scanAdjustment(row pgx.Row) (operations.Adjustment, error) {
	var (
		a               operations.Adjustment
		kind, ownerType string
	)

	err := row.Scan(
		&a.ID, &kind, &ownerType, &a.OwnerID, &a.CurrencyCode, &a.Amount,
		&a.TripID, &a.DriverID, &a.DriverAmount,
		&a.Reason, &a.TransactionID, &a.CreatedBy, &a.CreatedAt,
	)
	a.Kind = operations.Kind(kind)
	a.OwnerType = wallet.OwnerType(ownerType)

	return a, err
}

func (s *OperationsStore) FindAdjustmentByKey(ctx context.Context, createdBy, key string) (operations.Adjustment, bool, error) {
	a, err := scanAdjustment(s.wallets.pool.QueryRow(
		ctx,
		`SELECT `+adjustmentColumns+` FROM wallet_adjustments WHERE created_by = $1 AND idempotency_key = $2`,
		createdBy, key,
	))

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return operations.Adjustment{}, false, nil
	case err != nil:
		return operations.Adjustment{}, false, fmt.Errorf("select adjustment: %w", err)
	}

	return a, true, nil
}

// movementFor is a staff movement on a wallet: a rider's never goes below
// zero; a driver's may, and their suspension follows the new balance.
func movementFor(config wallet.Config, ownerType wallet.OwnerType, ownerID string, t wallet.TransactionType, amount wallet.Money, tripID, description string) wallet.MovementInput {
	input := wallet.MovementInput{
		OwnerType: ownerType, OwnerID: ownerID, Type: t, Amount: amount, TripID: tripID, Description: description,
	}

	if ownerType == wallet.OwnerDriver {
		floor := config.SuspensionFloor()
		input.AllowNegative = true
		input.SuspensionFloor = &floor
	}

	return input
}

func (s *OperationsStore) Adjust(ctx context.Context, record operations.AdjustRecord) (operations.Adjustment, wallet.Wallet, error) {
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
		config, record.OwnerType, record.OwnerID, wallet.TxAdjustment, record.Amount, "", "Adjustment: "+record.Reason,
	))
	if err != nil {
		return operations.Adjustment{}, wallet.Wallet{}, err
	}

	made, err := scanAdjustment(tx.QueryRow(
		ctx,
		`INSERT INTO wallet_adjustments
		    (kind, owner_type, owner_id, currency_code, amount, reason, transaction_id, created_by, idempotency_key)
		 VALUES ('adjustment', $1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING `+adjustmentColumns,
		string(record.OwnerType), record.OwnerID, target.CurrencyCode, record.Amount,
		record.Reason, transaction.ID, record.CreatedBy, record.IdempotencyKey,
	))
	if err != nil {
		if isUniqueViolation(err, "wallet_adjustments_creator_key_unique") {
			return operations.Adjustment{}, wallet.Wallet{}, wallet.ErrDuplicateRequest
		}

		return operations.Adjustment{}, wallet.Wallet{}, fmt.Errorf("insert adjustment: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return operations.Adjustment{}, wallet.Wallet{}, fmt.Errorf("commit transaction: %w", err)
	}

	return made, updated, nil
}

type settlementParties struct {
	riderID, driverID, currency string
	charged                     decimal.Decimal
}

func readSettlementParties(ctx context.Context, q pgx.Tx, tripID string, lock bool) (settlementParties, error) {
	query := `SELECT rider_id, driver_id, currency_code, fare_amount FROM trip_settlements WHERE trip_id = $1`
	if lock {
		query += ` FOR UPDATE`
	}

	var p settlementParties

	err := q.QueryRow(ctx, query, tripID).Scan(&p.riderID, &p.driverID, &p.currency, &p.charged)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return settlementParties{}, operations.ErrTripNotSettled
	case err != nil:
		return settlementParties{}, fmt.Errorf("select settlement: %w", err)
	}

	return p, nil
}

func (s *OperationsStore) Refund(ctx context.Context, record operations.RefundRecord) (operations.Adjustment, wallet.Wallet, error) {
	config, err := s.wallets.GetActiveConfig(ctx)
	if err != nil {
		return operations.Adjustment{}, wallet.Wallet{}, err
	}

	tx, err := s.wallets.pool.Begin(ctx)
	if err != nil {
		return operations.Adjustment{}, wallet.Wallet{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	parties, err := readSettlementParties(ctx, tx, record.TripID, false)
	if err != nil {
		return operations.Adjustment{}, wallet.Wallet{}, err
	}

	// The wallets first, then the settlement: the order money reaching a
	// rider's wallet takes when it pays their fees, so the two never wait on
	// each other.
	rider, err := ensureWalletTx(ctx, tx, wallet.OwnerRider, parties.riderID, parties.currency)
	if err != nil {
		return operations.Adjustment{}, wallet.Wallet{}, err
	}

	var driver wallet.Wallet
	if record.DriverAmount.IsPositive() {
		if driver, err = ensureWalletTx(ctx, tx, wallet.OwnerDriver, parties.driverID, parties.currency); err != nil {
			return operations.Adjustment{}, wallet.Wallet{}, err
		}
	}

	if _, err := readSettlementParties(ctx, tx, record.TripID, true); err != nil {
		return operations.Adjustment{}, wallet.Wallet{}, err
	}

	var refunded decimal.Decimal
	if err := tx.QueryRow(
		ctx,
		`SELECT COALESCE(sum(amount), 0) FROM wallet_adjustments WHERE trip_id = $1 AND kind = 'refund'`,
		record.TripID,
	).Scan(&refunded); err != nil {
		return operations.Adjustment{}, wallet.Wallet{}, fmt.Errorf("sum the trip's refunds: %w", err)
	}

	if refunded.Add(record.Amount).GreaterThan(parties.charged) {
		return operations.Adjustment{}, wallet.Wallet{}, operations.ErrRefundTooLarge
	}

	description := "Refund: " + record.Reason

	updated, transaction, err := applyMovementTx(ctx, tx, rider, movementFor(
		config, wallet.OwnerRider, parties.riderID, wallet.TxRefund, record.Amount, record.TripID, description,
	))
	if err != nil {
		return operations.Adjustment{}, wallet.Wallet{}, err
	}

	var driverID, driverTransactionID *string

	if record.DriverAmount.IsPositive() {
		_, taken, err := applyMovementTx(ctx, tx, driver, movementFor(
			config, wallet.OwnerDriver, parties.driverID, wallet.TxRefund, record.DriverAmount.Neg(), record.TripID, description,
		))
		if err != nil {
			return operations.Adjustment{}, wallet.Wallet{}, err
		}

		driverID, driverTransactionID = &parties.driverID, &taken.ID
	}

	made, err := scanAdjustment(tx.QueryRow(
		ctx,
		`INSERT INTO wallet_adjustments
		    (kind, owner_type, owner_id, currency_code, amount, trip_id, driver_id, driver_amount,
		     reason, transaction_id, driver_transaction_id, created_by, idempotency_key)
		 VALUES ('refund', 'rider', $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		 RETURNING `+adjustmentColumns,
		parties.riderID, parties.currency, record.Amount, record.TripID, driverID, record.DriverAmount,
		record.Reason, transaction.ID, driverTransactionID, record.CreatedBy, record.IdempotencyKey,
	))
	if err != nil {
		if isUniqueViolation(err, "wallet_adjustments_creator_key_unique") {
			return operations.Adjustment{}, wallet.Wallet{}, wallet.ErrDuplicateRequest
		}

		return operations.Adjustment{}, wallet.Wallet{}, fmt.Errorf("insert refund: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return operations.Adjustment{}, wallet.Wallet{}, fmt.Errorf("commit transaction: %w", err)
	}

	return made, updated, nil
}

func (s *OperationsStore) TripRefunds(ctx context.Context, tripID string) (operations.TripRefunds, error) {
	var charged decimal.Decimal

	err := s.wallets.pool.QueryRow(ctx, `SELECT fare_amount FROM trip_settlements WHERE trip_id = $1`, tripID).Scan(&charged)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return operations.TripRefunds{}, operations.ErrTripNotSettled
	case err != nil:
		return operations.TripRefunds{}, fmt.Errorf("select settlement: %w", err)
	}

	rows, err := s.wallets.pool.Query(
		ctx,
		`SELECT `+adjustmentColumns+` FROM wallet_adjustments WHERE trip_id = $1 AND kind = 'refund' ORDER BY created_at`,
		tripID,
	)
	if err != nil {
		return operations.TripRefunds{}, fmt.Errorf("select refunds: %w", err)
	}

	refunds, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (operations.Adjustment, error) { return scanAdjustment(row) })
	if err != nil {
		return operations.TripRefunds{}, fmt.Errorf("read refunds: %w", err)
	}

	out := operations.TripRefunds{Refunds: refunds, Charged: charged, Refunded: decimal.Zero}
	for _, r := range refunds {
		out.Refunded = out.Refunded.Add(r.Amount)
	}

	return out, nil
}

const payoutColumns = `id, driver_id, currency_code, amount, destination, status, idempotency_key, created_at,
        approved_at, paid_at, paid_reference, rejected_at, reject_reason`

func scanPayout(row pgx.Row) (operations.Payout, error) {
	var (
		p      operations.Payout
		status string
	)

	err := row.Scan(
		&p.ID, &p.DriverID, &p.CurrencyCode, &p.Amount, &p.Destination, &status, &p.IdempotencyKey, &p.CreatedAt,
		&p.ApprovedAt, &p.PaidAt, &p.PaidReference, &p.RejectedAt, &p.RejectReason,
	)
	p.Status = operations.PayoutStatus(status)

	return p, err
}

func (s *OperationsStore) FindPayoutByKey(ctx context.Context, driverID, key string) (operations.Payout, bool, error) {
	p, err := scanPayout(s.wallets.pool.QueryRow(
		ctx,
		`SELECT `+payoutColumns+` FROM payout_requests WHERE driver_id = $1 AND idempotency_key = $2`,
		driverID, key,
	))

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return operations.Payout{}, false, nil
	case err != nil:
		return operations.Payout{}, false, fmt.Errorf("select payout: %w", err)
	}

	return p, true, nil
}

func (s *OperationsStore) RequestPayout(ctx context.Context, record operations.PayoutRecord) (operations.Payout, wallet.Wallet, wallet.Transaction, error) {
	config, err := s.wallets.GetActiveConfig(ctx)
	if err != nil {
		return operations.Payout{}, wallet.Wallet{}, wallet.Transaction{}, err
	}

	tx, err := s.wallets.pool.Begin(ctx)
	if err != nil {
		return operations.Payout{}, wallet.Wallet{}, wallet.Transaction{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	driver, err := ensureWalletTx(ctx, tx, wallet.OwnerDriver, record.DriverID, config.CurrencyCode)
	if err != nil {
		return operations.Payout{}, wallet.Wallet{}, wallet.Transaction{}, err
	}

	if driver.Blocked {
		return operations.Payout{}, wallet.Wallet{}, wallet.Transaction{}, wallet.ErrWalletBlocked
	}

	// The hold: never below zero, whatever a driver's commission balance may
	// otherwise do.
	updated, hold, err := applyMovementTx(ctx, tx, driver, wallet.MovementInput{
		OwnerType: wallet.OwnerDriver, OwnerID: record.DriverID, Type: wallet.TxPayout,
		Amount: record.Amount.Neg(), Description: "Payout requested",
	})
	if err != nil {
		return operations.Payout{}, wallet.Wallet{}, wallet.Transaction{}, err
	}

	made, err := scanPayout(tx.QueryRow(
		ctx,
		`INSERT INTO payout_requests (driver_id, currency_code, amount, destination, idempotency_key, hold_transaction_id)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING `+payoutColumns,
		record.DriverID, driver.CurrencyCode, record.Amount, record.Destination, record.IdempotencyKey, hold.ID,
	))
	if err != nil {
		switch {
		case isUniqueViolation(err, "payout_requests_driver_key_unique"):
			return operations.Payout{}, wallet.Wallet{}, wallet.Transaction{}, wallet.ErrDuplicateRequest
		case isUniqueViolation(err, "payout_requests_one_open_per_driver"):
			return operations.Payout{}, wallet.Wallet{}, wallet.Transaction{}, operations.ErrPayoutOpen
		}

		return operations.Payout{}, wallet.Wallet{}, wallet.Transaction{}, fmt.Errorf("insert payout request: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return operations.Payout{}, wallet.Wallet{}, wallet.Transaction{}, fmt.Errorf("commit transaction: %w", err)
	}

	return made, updated, hold, nil
}

func (s *OperationsStore) GetPayout(ctx context.Context, id string) (operations.Payout, error) {
	p, err := scanPayout(s.wallets.pool.QueryRow(ctx, `SELECT `+payoutColumns+` FROM payout_requests WHERE id = $1`, id))

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return operations.Payout{}, operations.ErrPayoutNotFound
	case err != nil:
		return operations.Payout{}, fmt.Errorf("select payout: %w", err)
	}

	return p, nil
}

func (s *OperationsStore) ListPayouts(ctx context.Context, driverID string, status operations.PayoutStatus, offset, limit int) ([]operations.Payout, error) {
	order := `created_at, id`
	if driverID != "" {
		order = `created_at DESC, id DESC`
	}

	rows, err := s.wallets.pool.Query(
		ctx,
		`SELECT `+payoutColumns+` FROM payout_requests
		 WHERE ($1 = '' OR driver_id::text = $1) AND ($2 = '' OR status = $2)
		 ORDER BY `+order+`
		 OFFSET $3 LIMIT $4`,
		driverID, string(status), offset, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("select payouts: %w", err)
	}

	payouts, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (operations.Payout, error) { return scanPayout(row) })
	if err != nil {
		return nil, fmt.Errorf("read payouts: %w", err)
	}

	return payouts, nil
}

func lockPayout(ctx context.Context, tx pgx.Tx, id string) (operations.Payout, error) {
	p, err := scanPayout(tx.QueryRow(ctx, `SELECT `+payoutColumns+` FROM payout_requests WHERE id = $1 FOR UPDATE`, id))

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return operations.Payout{}, operations.ErrPayoutNotFound
	case err != nil:
		return operations.Payout{}, fmt.Errorf("lock payout: %w", err)
	}

	return p, nil
}

func (s *OperationsStore) SetPayoutStatus(ctx context.Context, id string, to operations.PayoutStatus, by, reference string, now time.Time) (operations.Payout, error) {
	tx, err := s.wallets.pool.Begin(ctx)
	if err != nil {
		return operations.Payout{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	current, err := lockPayout(ctx, tx, id)
	if err != nil {
		return operations.Payout{}, err
	}

	var update string

	switch {
	case to == operations.PayoutApproved && current.Status == operations.PayoutPending:
		update = `UPDATE payout_requests SET status = 'approved', approved_at = $2, reviewed_by = $3 WHERE id = $1 RETURNING ` + payoutColumns
	case to == operations.PayoutPaid && (current.Status == operations.PayoutPending || current.Status == operations.PayoutApproved):
		update = `UPDATE payout_requests SET status = 'paid', paid_at = $2, reviewed_by = $3, paid_reference = $4 WHERE id = $1 RETURNING ` + payoutColumns
	default:
		return operations.Payout{}, operations.ErrPayoutState
	}

	args := []any{id, now, by}
	if to == operations.PayoutPaid {
		args = append(args, reference)
	}

	moved, err := scanPayout(tx.QueryRow(ctx, update, args...))
	if err != nil {
		return operations.Payout{}, fmt.Errorf("update payout: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return operations.Payout{}, fmt.Errorf("commit transaction: %w", err)
	}

	return moved, nil
}

func (s *OperationsStore) RejectPayout(ctx context.Context, id, by, reason string, now time.Time) (operations.Payout, error) {
	config, err := s.wallets.GetActiveConfig(ctx)
	if err != nil {
		return operations.Payout{}, err
	}

	tx, err := s.wallets.pool.Begin(ctx)
	if err != nil {
		return operations.Payout{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Read it for the driver, lock the wallet, then the request: the order
	// a new request takes (wallet, then its row).
	current, err := s.GetPayout(ctx, id)
	if err != nil {
		return operations.Payout{}, err
	}

	driver, err := ensureWalletTx(ctx, tx, wallet.OwnerDriver, current.DriverID, current.CurrencyCode)
	if err != nil {
		return operations.Payout{}, err
	}

	if current, err = lockPayout(ctx, tx, id); err != nil {
		return operations.Payout{}, err
	}

	if current.Status != operations.PayoutPending && current.Status != operations.PayoutApproved {
		return operations.Payout{}, operations.ErrPayoutState
	}

	floor := config.SuspensionFloor()

	_, returned, err := applyMovementTx(ctx, tx, driver, wallet.MovementInput{
		OwnerType: wallet.OwnerDriver, OwnerID: current.DriverID, Type: wallet.TxPayoutReturn,
		Amount: current.Amount, Description: "Payout rejected: " + reason, SuspensionFloor: &floor,
	})
	if err != nil {
		return operations.Payout{}, err
	}

	rejected, err := scanPayout(tx.QueryRow(
		ctx,
		`UPDATE payout_requests
		 SET status = 'rejected', rejected_at = $2, reviewed_by = $3, reject_reason = $4, return_transaction_id = $5
		 WHERE id = $1
		 RETURNING `+payoutColumns,
		id, now, by, reason, returned.ID,
	))
	if err != nil {
		return operations.Payout{}, fmt.Errorf("reject payout: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return operations.Payout{}, fmt.Errorf("commit transaction: %w", err)
	}

	return rejected, nil
}

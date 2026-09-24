package postgres

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/voucher"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

// VoucherStore keeps voucher batches and redeems vouchers into riders'
// wallets, with the same ledger and locking as every other movement.
type VoucherStore struct {
	wallets *WalletRepository
}

var _ voucher.Store = (*VoucherStore)(nil)

func NewVoucherStore(wallets *WalletRepository) *VoucherStore {
	if wallets == nil {
		panic("wallet repository is required")
	}

	return &VoucherStore{wallets: wallets}
}

func (s *VoucherStore) Config(ctx context.Context) (wallet.Config, error) {
	return s.wallets.GetActiveConfig(ctx)
}

// batchColumns is every batch column with its counts, in scanBatch's order,
// over voucher_batches b.
const batchColumns = `b.id, b.number, b.label, b.seller, b.amount, b.currency_code, b.quantity,
        b.status, b.expires_at, b.created_by, b.idempotency_key, b.created_at,
        b.exported_at, b.cancelled_at, b.cancel_reason,
        (SELECT count(*) FROM vouchers v WHERE v.batch_id = b.id AND v.status = 'redeemed'),
        (SELECT count(*) FROM vouchers v WHERE v.batch_id = b.id AND v.status = 'void')`

func scanBatch(row pgx.Row) (voucher.Batch, error) {
	var (
		b             voucher.Batch
		status        string
		redeemed, vo  int64
		exported, can *time.Time
	)

	err := row.Scan(
		&b.ID, &b.Number, &b.Label, &b.Seller, &b.Amount, &b.CurrencyCode, &b.Quantity,
		&status, &b.ExpiresAt, &b.CreatedBy, &b.IdempotencyKey, &b.CreatedAt,
		&exported, &can, &b.CancelReason,
		&redeemed, &vo,
	)
	if err != nil {
		return voucher.Batch{}, err
	}

	b.Status = voucher.BatchStatus(status)
	b.ExportedAt = exported
	b.CancelledAt = can
	b.RedeemedCount = int(redeemed)
	b.VoidCount = int(vo)
	b.RedeemedAmount = b.Amount.Mul(decimal.NewFromInt(redeemed))

	return b, nil
}

func (s *VoucherStore) CreateBatch(ctx context.Context, batch voucher.Batch, codes []voucher.Sealed) (voucher.Batch, error) {
	tx, err := s.wallets.pool.Begin(ctx)
	if err != nil {
		return voucher.Batch{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var (
		id     string
		number int64
	)

	err = tx.QueryRow(
		ctx,
		`INSERT INTO voucher_batches
		    (label, seller, currency_code, amount, quantity, expires_at, created_by, idempotency_key)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING id, number`,
		batch.Label, batch.Seller, batch.CurrencyCode, batch.Amount, len(codes),
		batch.ExpiresAt, batch.CreatedBy, batch.IdempotencyKey,
	).Scan(&id, &number)
	if err != nil {
		if isUniqueViolation(err, "voucher_batches_creator_key_unique") {
			return voucher.Batch{}, wallet.ErrDuplicateRequest
		}

		return voucher.Batch{}, fmt.Errorf("insert voucher batch: %w", err)
	}

	rows := make([][]any, 0, len(codes))
	for i, code := range codes {
		rows = append(rows, []any{id, voucher.Serial(number, i+1), code.Hash, code.Sealed})
	}

	if _, err := tx.CopyFrom(
		ctx,
		pgx.Identifier{"vouchers"},
		[]string{"batch_id", "serial", "code_hash", "code_sealed"},
		pgx.CopyFromRows(rows),
	); err != nil {
		if isUniqueViolation(err, "vouchers_code_hash_unique") {
			return voucher.Batch{}, voucher.ErrCodeTaken
		}

		return voucher.Batch{}, fmt.Errorf("insert vouchers: %w", err)
	}

	created, err := scanBatch(tx.QueryRow(ctx, `SELECT `+batchColumns+` FROM voucher_batches b WHERE b.id = $1`, id))
	if err != nil {
		return voucher.Batch{}, fmt.Errorf("read the new voucher batch: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return voucher.Batch{}, fmt.Errorf("commit transaction: %w", err)
	}

	return created, nil
}

func (s *VoucherStore) FindBatchByKey(ctx context.Context, createdBy, key string) (voucher.Batch, bool, error) {
	b, err := scanBatch(s.wallets.pool.QueryRow(
		ctx,
		`SELECT `+batchColumns+` FROM voucher_batches b WHERE b.created_by = $1 AND b.idempotency_key = $2`,
		createdBy, key,
	))

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return voucher.Batch{}, false, nil
	case err != nil:
		return voucher.Batch{}, false, fmt.Errorf("select voucher batch: %w", err)
	}

	return b, true, nil
}

func (s *VoucherStore) GetBatch(ctx context.Context, id string) (voucher.Batch, error) {
	if !isUUID(id) {
		return voucher.Batch{}, voucher.ErrBatchNotFound
	}

	b, err := scanBatch(s.wallets.pool.QueryRow(ctx, `SELECT `+batchColumns+` FROM voucher_batches b WHERE b.id = $1`, id))

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return voucher.Batch{}, voucher.ErrBatchNotFound
	case err != nil:
		return voucher.Batch{}, fmt.Errorf("select voucher batch: %w", err)
	}

	return b, nil
}

func (s *VoucherStore) ListBatches(ctx context.Context, status voucher.BatchStatus, offset, limit int) ([]voucher.Batch, error) {
	rows, err := s.wallets.pool.Query(
		ctx,
		`SELECT `+batchColumns+` FROM voucher_batches b
		 WHERE ($1 = '' OR b.status = $1)
		 ORDER BY b.created_at DESC, b.number DESC
		 OFFSET $2 LIMIT $3`,
		string(status), offset, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("select voucher batches: %w", err)
	}

	batches, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (voucher.Batch, error) { return scanBatch(row) })
	if err != nil {
		return nil, fmt.Errorf("read voucher batches: %w", err)
	}

	return batches, nil
}

func (s *VoucherStore) ExportBatch(
	ctx context.Context,
	id, by string,
	now time.Time,
	open func(hash, sealed []byte) (string, error),
) (voucher.Batch, []voucher.Exported, error) {
	if !isUUID(id) {
		return voucher.Batch{}, nil, voucher.ErrBatchNotFound
	}

	tx, err := s.wallets.pool.Begin(ctx)
	if err != nil {
		return voucher.Batch{}, nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := lockBatchInStatus(ctx, tx, id, voucher.BatchCreated, voucher.ErrNotExportable); err != nil {
		return voucher.Batch{}, nil, err
	}

	rows, err := tx.Query(
		ctx,
		`SELECT serial, code_hash, code_sealed FROM vouchers
		 WHERE batch_id = $1 ORDER BY serial`,
		id,
	)
	if err != nil {
		return voucher.Batch{}, nil, fmt.Errorf("select sealed vouchers: %w", err)
	}

	type sealedRow struct {
		serial       string
		hash, sealed []byte
	}

	sealed, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (sealedRow, error) {
		var r sealedRow
		err := row.Scan(&r.serial, &r.hash, &r.sealed)

		return r, err
	})
	if err != nil {
		return voucher.Batch{}, nil, fmt.Errorf("read sealed vouchers: %w", err)
	}

	exported := make([]voucher.Exported, 0, len(sealed))

	for _, r := range sealed {
		code, err := open(r.hash, r.sealed)
		if err != nil {
			return voucher.Batch{}, nil, err
		}

		exported = append(exported, voucher.Exported{Serial: r.serial, Code: code})
	}

	if _, err := tx.Exec(ctx, `UPDATE vouchers SET code_sealed = NULL WHERE batch_id = $1`, id); err != nil {
		return voucher.Batch{}, nil, fmt.Errorf("wipe the sealed codes: %w", err)
	}

	if _, err := tx.Exec(
		ctx,
		`UPDATE voucher_batches SET status = 'exported', exported_at = $2, exported_by = $3 WHERE id = $1`,
		id, now, by,
	); err != nil {
		return voucher.Batch{}, nil, fmt.Errorf("mark the batch exported: %w", err)
	}

	batch, err := scanBatch(tx.QueryRow(ctx, `SELECT `+batchColumns+` FROM voucher_batches b WHERE b.id = $1`, id))
	if err != nil {
		return voucher.Batch{}, nil, fmt.Errorf("read the exported batch: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return voucher.Batch{}, nil, fmt.Errorf("commit transaction: %w", err)
	}

	return batch, exported, nil
}

// lockBatchInStatus locks the batch; wrong is returned when it is not in
// status (for a cancel, status "" means anything but cancelled).
func lockBatchInStatus(ctx context.Context, tx pgx.Tx, id string, status voucher.BatchStatus, wrong error) error {
	var current string

	err := tx.QueryRow(ctx, `SELECT status FROM voucher_batches WHERE id = $1 FOR UPDATE`, id).Scan(&current)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return voucher.ErrBatchNotFound
	case err != nil:
		return fmt.Errorf("lock voucher batch: %w", err)
	case status == "" && current == string(voucher.BatchCancelled):
		return wrong
	case status != "" && current != string(status):
		return wrong
	}

	return nil
}

func (s *VoucherStore) CancelBatch(ctx context.Context, id, by, reason string, now time.Time) (voucher.Batch, error) {
	if !isUUID(id) {
		return voucher.Batch{}, voucher.ErrBatchNotFound
	}

	tx, err := s.wallets.pool.Begin(ctx)
	if err != nil {
		return voucher.Batch{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := lockBatchInStatus(ctx, tx, id, "", voucher.ErrNotCancellable); err != nil {
		return voucher.Batch{}, err
	}

	if _, err := tx.Exec(ctx, `UPDATE vouchers SET code_sealed = NULL WHERE batch_id = $1 AND code_sealed IS NOT NULL`, id); err != nil {
		return voucher.Batch{}, fmt.Errorf("wipe the sealed codes: %w", err)
	}

	if _, err := tx.Exec(
		ctx,
		`UPDATE voucher_batches
		 SET status = 'cancelled', cancelled_at = $2, cancelled_by = $3, cancel_reason = $4
		 WHERE id = $1`,
		id, now, by, reason,
	); err != nil {
		return voucher.Batch{}, fmt.Errorf("cancel the batch: %w", err)
	}

	batch, err := scanBatch(tx.QueryRow(ctx, `SELECT `+batchColumns+` FROM voucher_batches b WHERE b.id = $1`, id))
	if err != nil {
		return voucher.Batch{}, fmt.Errorf("read the cancelled batch: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return voucher.Batch{}, fmt.Errorf("commit transaction: %w", err)
	}

	return batch, nil
}

// voucherColumns is a voucher with its batch, in scanVoucher's order, over
// vouchers v JOIN voucher_batches b.
const voucherColumns = `v.id, v.serial, b.id, b.label, b.status, b.amount, b.currency_code, v.status,
        b.expires_at, COALESCE(v.redeemed_by_rider_id::text, ''), v.redeemed_at,
        COALESCE(v.transaction_id::text, ''), v.voided_at, v.void_reason`

func scanVoucher(row pgx.Row) (voucher.Voucher, error) {
	var (
		v                    voucher.Voucher
		batchStatus, vStatus string
		redeemedAt, voidedAt *time.Time
	)

	err := row.Scan(
		&v.ID, &v.Serial, &v.BatchID, &v.BatchLabel, &batchStatus, &v.Amount, &v.CurrencyCode, &vStatus,
		&v.ExpiresAt, &v.RedeemedByRiderID, &redeemedAt,
		&v.TransactionID, &voidedAt, &v.VoidReason,
	)
	if err != nil {
		return voucher.Voucher{}, err
	}

	v.BatchStatus = voucher.BatchStatus(batchStatus)
	v.Status = voucher.Status(vStatus)
	v.RedeemedAt = redeemedAt
	v.VoidedAt = voidedAt

	return v, nil
}

func (s *VoucherStore) GetVoucher(ctx context.Context, serial string) (voucher.Voucher, error) {
	v, err := scanVoucher(s.wallets.pool.QueryRow(
		ctx,
		`SELECT `+voucherColumns+` FROM vouchers v JOIN voucher_batches b ON b.id = v.batch_id WHERE v.serial = $1`,
		serial,
	))

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return voucher.Voucher{}, voucher.ErrVoucherNotFound
	case err != nil:
		return voucher.Voucher{}, fmt.Errorf("select voucher: %w", err)
	}

	return v, nil
}

func (s *VoucherStore) VoidVoucher(ctx context.Context, serial, by, reason string, now time.Time) (voucher.Voucher, error) {
	tx, err := s.wallets.pool.Begin(ctx)
	if err != nil {
		return voucher.Voucher{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	found, err := scanVoucher(tx.QueryRow(
		ctx,
		`SELECT `+voucherColumns+` FROM vouchers v JOIN voucher_batches b ON b.id = v.batch_id
		 WHERE v.serial = $1 FOR UPDATE OF v`,
		serial,
	))

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return voucher.Voucher{}, voucher.ErrVoucherNotFound
	case err != nil:
		return voucher.Voucher{}, fmt.Errorf("lock voucher: %w", err)
	case found.Status != voucher.Available:
		return voucher.Voucher{}, voucher.ErrNotVoidable
	}

	if _, err := tx.Exec(
		ctx,
		`UPDATE vouchers SET status = 'void', voided_at = $2, voided_by = $3, void_reason = $4 WHERE id = $1`,
		found.ID, now, by, reason,
	); err != nil {
		return voucher.Voucher{}, fmt.Errorf("void voucher: %w", err)
	}

	voided, err := scanVoucher(tx.QueryRow(
		ctx,
		`SELECT `+voucherColumns+` FROM vouchers v JOIN voucher_batches b ON b.id = v.batch_id WHERE v.id = $1`,
		found.ID,
	))
	if err != nil {
		return voucher.Voucher{}, fmt.Errorf("read the voided voucher: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return voucher.Voucher{}, fmt.Errorf("commit transaction: %w", err)
	}

	return voided, nil
}

func (s *VoucherStore) Redeem(ctx context.Context, riderID string, hash []byte, now time.Time) (voucher.Redemption, error) {
	tx, err := s.wallets.pool.Begin(ctx)
	if err != nil {
		return voucher.Redemption{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	found, err := scanVoucher(tx.QueryRow(
		ctx,
		`SELECT `+voucherColumns+` FROM vouchers v JOIN voucher_batches b ON b.id = v.batch_id
		 WHERE v.code_hash = $1 FOR UPDATE OF v`,
		hash,
	))

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return voucher.Redemption{}, voucher.ErrCodeNotValid
	case err != nil:
		return voucher.Redemption{}, fmt.Errorf("lock voucher: %w", err)
	}

	redemption := voucher.Redemption{Serial: found.Serial, Amount: found.Amount, CurrencyCode: found.CurrencyCode}

	switch {
	case found.BatchStatus == voucher.BatchCreated:
		// Never handed to a seller: as if it did not exist.
		return voucher.Redemption{}, voucher.ErrCodeNotValid

	case found.Status == voucher.Redeemed && found.RedeemedByRiderID == riderID:
		// The same rider again (a retry): their redemption, as it was.
		redemption.Wallet, redemption.Transaction, err = redeemedBefore(ctx, tx, riderID, found.TransactionID)
		if err != nil {
			return voucher.Redemption{}, err
		}

		return redemption, nil

	case found.Status == voucher.Redeemed:
		return voucher.Redemption{}, voucher.ErrCodeUsed

	case found.Status == voucher.Void || found.BatchStatus == voucher.BatchCancelled:
		return voucher.Redemption{}, voucher.ErrCodeCancelled

	case !now.Before(found.ExpiresAt):
		return voucher.Redemption{}, voucher.ErrCodeExpired
	}

	rider, err := ensureWalletTx(ctx, tx, wallet.OwnerRider, riderID, found.CurrencyCode)
	if err != nil {
		return voucher.Redemption{}, err
	}

	if rider.CurrencyCode != found.CurrencyCode {
		return voucher.Redemption{}, voucher.ErrCurrencyDiffer
	}

	updated, transaction, err := applyMovementTx(ctx, tx, rider, wallet.MovementInput{
		OwnerType:      wallet.OwnerRider,
		OwnerID:        riderID,
		Type:           wallet.TxVoucher,
		Amount:         found.Amount,
		IdempotencyKey: "voucher:" + found.ID,
		Description:    "Voucher " + found.Serial,
	})
	if err != nil {
		return voucher.Redemption{}, err
	}

	if _, err := tx.Exec(
		ctx,
		`UPDATE vouchers
		 SET status = 'redeemed', redeemed_by_rider_id = $2, redeemed_at = $3, transaction_id = $4
		 WHERE id = $1`,
		found.ID, riderID, now, transaction.ID,
	); err != nil {
		return voucher.Redemption{}, fmt.Errorf("mark the voucher redeemed: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return voucher.Redemption{}, fmt.Errorf("commit transaction: %w", err)
	}

	redemption.Wallet = updated
	redemption.Transaction = transaction

	return redemption, nil
}

// redeemedBefore is a rider's wallet now and the ledger row of the voucher
// they redeemed before.
func redeemedBefore(ctx context.Context, tx pgx.Tx, riderID, transactionID string) (wallet.Wallet, wallet.Transaction, error) {
	current, err := scanWallet(tx.QueryRow(
		ctx,
		walletSelectSQL+` WHERE owner_type = $1 AND owner_id = $2`,
		string(wallet.OwnerRider), riderID,
	))
	if err != nil {
		return wallet.Wallet{}, wallet.Transaction{}, fmt.Errorf("select wallet: %w", err)
	}

	var (
		t       wallet.Transaction
		txnType string
	)

	err = tx.QueryRow(
		ctx,
		`SELECT id, wallet_id, type, amount, balance_after,
		        COALESCE(trip_id::text, ''), COALESCE(transfer_id::text, ''),
		        COALESCE(description, ''), created_at
		 FROM wallet_transactions WHERE id = $1`,
		transactionID,
	).Scan(&t.ID, &t.WalletID, &txnType, &t.Amount, &t.BalanceAfter, &t.TripID, &t.TransferID, &t.Description, &t.CreatedAt)
	if err != nil {
		return wallet.Wallet{}, wallet.Transaction{}, fmt.Errorf("select the voucher's ledger row: %w", err)
	}

	t.Type = wallet.TransactionType(txnType)

	return current, t, nil
}

func (s *VoucherStore) Failures(ctx context.Context, riderID string, since time.Time) (int, time.Time, error) {
	var (
		count  int
		oldest *time.Time
	)

	err := s.wallets.pool.QueryRow(
		ctx,
		`SELECT count(*), min(failed_at) FROM voucher_redeem_failures WHERE rider_id = $1 AND failed_at > $2`,
		riderID, since,
	).Scan(&count, &oldest)
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("count voucher failures: %w", err)
	}

	if oldest == nil {
		return count, time.Time{}, nil
	}

	return count, *oldest, nil
}

func (s *VoucherStore) RecordFailure(ctx context.Context, riderID string, at, forgetBefore time.Time) error {
	if _, err := s.wallets.pool.Exec(
		ctx,
		`WITH forgotten AS (
		     DELETE FROM voucher_redeem_failures WHERE rider_id = $1 AND failed_at <= $3
		 )
		 INSERT INTO voucher_redeem_failures (rider_id, failed_at) VALUES ($1, $2)`,
		riderID, at, forgetBefore,
	); err != nil {
		return fmt.Errorf("record a voucher failure: %w", err)
	}

	return nil
}

func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError

	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode && pgErr.ConstraintName == constraint
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// isUUID: anything else is no batch id (and would be a cast error).
func isUUID(id string) bool {
	return uuidPattern.MatchString(id)
}

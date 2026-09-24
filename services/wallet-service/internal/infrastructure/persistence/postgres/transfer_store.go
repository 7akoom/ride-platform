package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/transfer"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

// TransferStore moves money between riders' wallets, with the same ledger
// and locking as every other movement.
type TransferStore struct {
	wallets *WalletRepository
}

var _ transfer.Store = (*TransferStore)(nil)

func NewTransferStore(wallets *WalletRepository) *TransferStore {
	if wallets == nil {
		panic("wallet repository is required")
	}

	return &TransferStore{wallets: wallets}
}

// transferColumns is every transfer column, in scanTransfer's order.
const transferColumns = `id, sender_rider_id, recipient_rider_id, sender_phone, recipient_phone,
        currency_code, amount, note, idempotency_key, created_at`

func scanTransfer(row pgx.Row) (transfer.Transfer, error) {
	var t transfer.Transfer

	err := row.Scan(
		&t.ID,
		&t.SenderRiderID,
		&t.RecipientRiderID,
		&t.SenderPhone,
		&t.RecipientPhone,
		&t.CurrencyCode,
		&t.Amount,
		&t.Note,
		&t.IdempotencyKey,
		&t.CreatedAt,
	)

	return t, err
}

func (r *TransferStore) Config(ctx context.Context) (wallet.Config, error) {
	return r.wallets.GetActiveConfig(ctx)
}

func (r *TransferStore) FindByKey(ctx context.Context, senderRiderID, key string) (transfer.Transfer, bool, error) {
	t, err := scanTransfer(r.wallets.pool.QueryRow(
		ctx,
		`SELECT `+transferColumns+` FROM wallet_transfers WHERE sender_rider_id = $1 AND idempotency_key = $2`,
		senderRiderID, key,
	))

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return transfer.Transfer{}, false, nil
	case err != nil:
		return transfer.Transfer{}, false, fmt.Errorf("select transfer: %w", err)
	}

	return t, true, nil
}

// Send locks both riders' wallets (in a fixed order, so two riders sending
// to each other at once cannot deadlock), checks the sender's last 24 hours
// against the limits, and records the transfer, both ledger rows and the
// wallet.transfer_completed event in one transaction.
func (r *TransferStore) Send(ctx context.Context, record transfer.Record) (transfer.Transfer, wallet.Wallet, error) {
	t := record.Transfer

	tx, err := r.wallets.pool.Begin(ctx)
	if err != nil {
		return transfer.Transfer{}, wallet.Wallet{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	first, second := t.SenderRiderID, t.RecipientRiderID
	if second < first {
		first, second = second, first
	}

	locked := map[string]wallet.Wallet{}

	for _, riderID := range []string{first, second} {
		w, err := ensureWalletTx(ctx, tx, wallet.OwnerRider, riderID, t.CurrencyCode)
		if err != nil {
			return transfer.Transfer{}, wallet.Wallet{}, err
		}

		locked[riderID] = w
	}

	sender, recipient := locked[t.SenderRiderID], locked[t.RecipientRiderID]

	if sender.Blocked {
		return transfer.Transfer{}, wallet.Wallet{}, wallet.ErrWalletBlocked
	}

	var (
		sentAmount decimal.Decimal
		sentCount  int
	)

	if err := tx.QueryRow(
		ctx,
		`SELECT COALESCE(SUM(amount), 0), COUNT(*)
		 FROM wallet_transfers
		 WHERE sender_rider_id = $1 AND created_at > CURRENT_TIMESTAMP - interval '24 hours'`,
		t.SenderRiderID,
	).Scan(&sentAmount, &sentCount); err != nil {
		return transfer.Transfer{}, wallet.Wallet{}, fmt.Errorf("sum the last day's transfers: %w", err)
	}

	switch {
	case sentCount >= record.DailyCount:
		return transfer.Transfer{}, wallet.Wallet{}, transfer.ErrDailyCount
	case sentAmount.Add(t.Amount).GreaterThan(record.DailyAmount):
		return transfer.Transfer{}, wallet.Wallet{}, transfer.ErrDailyLimit
	}

	stored, err := scanTransfer(tx.QueryRow(
		ctx,
		`INSERT INTO wallet_transfers
		    (sender_rider_id, recipient_rider_id, sender_phone, recipient_phone,
		     currency_code, amount, note, idempotency_key)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING `+transferColumns,
		t.SenderRiderID,
		t.RecipientRiderID,
		t.SenderPhone,
		t.RecipientPhone,
		t.CurrencyCode,
		t.Amount,
		t.Note,
		t.IdempotencyKey,
	))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode {
			return transfer.Transfer{}, wallet.Wallet{}, wallet.ErrDuplicateRequest
		}

		return transfer.Transfer{}, wallet.Wallet{}, fmt.Errorf("insert transfer: %w", err)
	}

	senderAfter, _, err := applyMovementTx(ctx, tx, sender, wallet.MovementInput{
		OwnerType:   wallet.OwnerRider,
		OwnerID:     t.SenderRiderID,
		Type:        wallet.TxTransferOut,
		Amount:      t.Amount.Neg(),
		TransferID:  stored.ID,
		Description: "Transfer to " + t.RecipientPhone,
	})
	if err != nil {
		return transfer.Transfer{}, wallet.Wallet{}, err
	}

	from := t.SenderPhone
	if from == "" {
		from = "a rider"
	}

	if _, _, err := applyMovementTx(ctx, tx, recipient, wallet.MovementInput{
		OwnerType:   wallet.OwnerRider,
		OwnerID:     t.RecipientRiderID,
		Type:        wallet.TxTransferIn,
		Amount:      t.Amount,
		TransferID:  stored.ID,
		Description: "Transfer from " + from,
	}); err != nil {
		return transfer.Transfer{}, wallet.Wallet{}, err
	}

	payload, err := json.Marshal(map[string]any{
		"transfer_id":        stored.ID,
		"sender_rider_id":    stored.SenderRiderID,
		"recipient_rider_id": stored.RecipientRiderID,
		"amount":             stored.Amount.String(),
		"currency_code":      stored.CurrencyCode,
		"sender_phone":       transfer.MaskPhone(stored.SenderPhone),
		"note":               stored.Note,
	})
	if err != nil {
		return transfer.Transfer{}, wallet.Wallet{}, fmt.Errorf("marshal wallet.transfer_completed payload: %w", err)
	}

	if _, err := tx.Exec(
		ctx,
		`INSERT INTO outbox_events
		    (aggregate_type, aggregate_id, event_type, schema_version,
		     payload, occurred_at, available_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $6)`,
		"transfer",
		stored.ID,
		"wallet.transfer_completed",
		schemaVersion,
		payload,
		time.Now().UTC(),
	); err != nil {
		return transfer.Transfer{}, wallet.Wallet{}, fmt.Errorf("insert wallet.transfer_completed outbox event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return transfer.Transfer{}, wallet.Wallet{}, fmt.Errorf("commit transaction: %w", err)
	}

	return stored, senderAfter, nil
}

func (r *TransferStore) List(ctx context.Context, riderID string, offset, limit int) ([]transfer.Transfer, error) {
	rows, err := r.wallets.pool.Query(
		ctx,
		`SELECT `+transferColumns+`
		 FROM wallet_transfers
		 WHERE sender_rider_id = $1 OR recipient_rider_id = $1
		 ORDER BY created_at DESC, id
		 OFFSET $2 LIMIT $3`,
		riderID, offset, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("select transfers: %w", err)
	}

	transfers, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (transfer.Transfer, error) {
		return scanTransfer(row)
	})
	if err != nil {
		return nil, fmt.Errorf("read transfers: %w", err)
	}

	return transfers, nil
}

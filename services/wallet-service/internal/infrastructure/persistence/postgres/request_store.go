package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/transfer"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

var _ transfer.Requests = (*TransferStore)(nil)

// requestColumns is every money request column, in scanRequest's order.
const requestColumns = `id, code, requester_rider_id, requester_phone,
        COALESCE(payer_rider_id::text, ''), COALESCE(payer_phone, ''),
        is_open, currency_code, amount, note, status,
        COALESCE(transfer_id::text, ''), idempotency_key, expires_at, created_at, closed_at`

func scanRequest(row pgx.Row) (transfer.MoneyRequest, error) {
	var (
		r      transfer.MoneyRequest
		status string
	)

	err := row.Scan(
		&r.ID,
		&r.Code,
		&r.RequesterRiderID,
		&r.RequesterPhone,
		&r.PayerRiderID,
		&r.PayerPhone,
		&r.Open,
		&r.CurrencyCode,
		&r.Amount,
		&r.Note,
		&status,
		&r.TransferID,
		&r.IdempotencyKey,
		&r.ExpiresAt,
		&r.CreatedAt,
		&r.ClosedAt,
	)
	r.Status = transfer.RequestStatus(status)

	return r, err
}

func (r *TransferStore) CreateRequest(ctx context.Context, request transfer.MoneyRequest) (transfer.MoneyRequest, error) {
	tx, err := r.wallets.pool.Begin(ctx)
	if err != nil {
		return transfer.MoneyRequest{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var payerRider, payerPhone any
	if !request.Open {
		payerRider, payerPhone = request.PayerRiderID, request.PayerPhone
	}

	created, err := scanRequest(tx.QueryRow(
		ctx,
		`INSERT INTO money_requests
		    (code, requester_rider_id, requester_phone, payer_rider_id, payer_phone, is_open,
		     currency_code, amount, note, idempotency_key, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		 RETURNING `+requestColumns,
		request.Code,
		request.RequesterRiderID,
		request.RequesterPhone,
		payerRider,
		payerPhone,
		request.Open,
		request.CurrencyCode,
		request.Amount,
		request.Note,
		request.IdempotencyKey,
		request.ExpiresAt,
	))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode {
			if pgErr.ConstraintName == "money_requests_code_unique" {
				return transfer.MoneyRequest{}, transfer.ErrCodeTaken
			}

			return transfer.MoneyRequest{}, wallet.ErrDuplicateRequest
		}

		return transfer.MoneyRequest{}, fmt.Errorf("insert money request: %w", err)
	}

	// The rider it is for is told.
	if !created.Open {
		payload, err := json.Marshal(map[string]any{
			"request_id":      created.ID,
			"code":            created.Code,
			"payer_rider_id":  created.PayerRiderID,
			"requester_phone": transfer.MaskPhone(created.RequesterPhone),
			"amount":          created.Amount.String(),
			"currency_code":   created.CurrencyCode,
			"note":            created.Note,
		})
		if err != nil {
			return transfer.MoneyRequest{}, fmt.Errorf("marshal wallet.money_requested payload: %w", err)
		}

		if _, err := tx.Exec(
			ctx,
			`INSERT INTO outbox_events
			    (aggregate_type, aggregate_id, event_type, schema_version,
			     payload, occurred_at, available_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $6)`,
			"money_request", created.ID, "wallet.money_requested", schemaVersion, payload, time.Now().UTC(),
		); err != nil {
			return transfer.MoneyRequest{}, fmt.Errorf("insert wallet.money_requested outbox event: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return transfer.MoneyRequest{}, fmt.Errorf("commit transaction: %w", err)
	}

	return created, nil
}

func (r *TransferStore) findRequest(ctx context.Context, where string, args ...any) (transfer.MoneyRequest, bool, error) {
	request, err := scanRequest(r.wallets.pool.QueryRow(ctx, `SELECT `+requestColumns+` FROM money_requests WHERE `+where, args...))

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return transfer.MoneyRequest{}, false, nil
	case err != nil:
		return transfer.MoneyRequest{}, false, fmt.Errorf("select money request: %w", err)
	}

	return request, true, nil
}

func (r *TransferStore) FindRequestByCode(ctx context.Context, code string) (transfer.MoneyRequest, bool, error) {
	return r.findRequest(ctx, `code = $1`, code)
}

func (r *TransferStore) FindRequestByKey(ctx context.Context, requesterRiderID, key string) (transfer.MoneyRequest, bool, error) {
	return r.findRequest(ctx, `requester_rider_id = $1 AND idempotency_key = $2`, requesterRiderID, key)
}

func (r *TransferStore) ListRequests(
	ctx context.Context,
	riderID, role string,
	status transfer.RequestStatus,
	now time.Time,
	offset, limit int,
) ([]transfer.MoneyRequest, error) {
	side := `requester_rider_id = $1`
	if role == "incoming" {
		side = `payer_rider_id = $1`
	}

	rows, err := r.wallets.pool.Query(
		ctx,
		`SELECT `+requestColumns+`
		 FROM money_requests
		 WHERE `+side+`
		   AND CASE $2
		         WHEN '' THEN true
		         WHEN 'expired' THEN status = 'pending' AND expires_at <= $3
		         WHEN 'pending' THEN status = 'pending' AND expires_at > $3
		         ELSE status = $2
		       END
		 ORDER BY created_at DESC, id
		 OFFSET $4 LIMIT $5`,
		riderID, string(status), now, offset, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("select money requests: %w", err)
	}

	requests, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (transfer.MoneyRequest, error) {
		return scanRequest(row)
	})
	if err != nil {
		return nil, fmt.Errorf("read money requests: %w", err)
	}

	return requests, nil
}

func (r *TransferStore) CloseRequest(
	ctx context.Context,
	requestID string,
	status transfer.RequestStatus,
	now time.Time,
) (transfer.MoneyRequest, error) {
	closed, err := scanRequest(r.wallets.pool.QueryRow(
		ctx,
		`UPDATE money_requests
		 SET status = $2, closed_at = $3
		 WHERE id = $1 AND status = 'pending' AND expires_at > $3
		 RETURNING `+requestColumns,
		requestID, string(status), now,
	))

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return transfer.MoneyRequest{}, transfer.ErrRequestNotPending
	case err != nil:
		return transfer.MoneyRequest{}, fmt.Errorf("close money request: %w", err)
	}

	return closed, nil
}

func (r *TransferStore) PayRequest(
	ctx context.Context,
	requestID string,
	payment transfer.Record,
	now time.Time,
) (transfer.MoneyRequest, transfer.Transfer, wallet.Wallet, error) {
	tx, err := r.wallets.pool.Begin(ctx)
	if err != nil {
		return transfer.MoneyRequest{}, transfer.Transfer{}, wallet.Wallet{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var pending bool
	if err := tx.QueryRow(
		ctx,
		`SELECT status = 'pending' AND expires_at > $2 FROM money_requests WHERE id = $1 FOR UPDATE`,
		requestID, now,
	).Scan(&pending); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return transfer.MoneyRequest{}, transfer.Transfer{}, wallet.Wallet{}, transfer.ErrRequestNotFound
		}

		return transfer.MoneyRequest{}, transfer.Transfer{}, wallet.Wallet{}, fmt.Errorf("lock money request: %w", err)
	}

	if !pending {
		return transfer.MoneyRequest{}, transfer.Transfer{}, wallet.Wallet{}, transfer.ErrRequestNotPending
	}

	sent, payer, err := sendTx(ctx, tx, payment)
	if err != nil {
		return transfer.MoneyRequest{}, transfer.Transfer{}, wallet.Wallet{}, err
	}

	paid, err := scanRequest(tx.QueryRow(
		ctx,
		`UPDATE money_requests
		 SET status = 'paid', payer_rider_id = $2, payer_phone = $3, transfer_id = $4, closed_at = $5
		 WHERE id = $1
		 RETURNING `+requestColumns,
		requestID, sent.SenderRiderID, sent.SenderPhone, sent.ID, now,
	))
	if err != nil {
		return transfer.MoneyRequest{}, transfer.Transfer{}, wallet.Wallet{}, fmt.Errorf("mark money request paid: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return transfer.MoneyRequest{}, transfer.Transfer{}, wallet.Wallet{}, fmt.Errorf("commit transaction: %w", err)
	}

	return paid, sent, payer, nil
}

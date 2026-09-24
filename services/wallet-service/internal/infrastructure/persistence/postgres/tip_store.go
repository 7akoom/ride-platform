package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/tips"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

// TipStore moves tips from riders' wallets to drivers', with the same ledger
// and locking as every other movement.
type TipStore struct {
	wallets *WalletRepository
}

var _ tips.Store = (*TipStore)(nil)

func NewTipStore(wallets *WalletRepository) *TipStore {
	if wallets == nil {
		panic("wallet repository is required")
	}

	return &TipStore{wallets: wallets}
}

func (s *TipStore) Config(ctx context.Context) (wallet.Config, error) {
	return s.wallets.GetActiveConfig(ctx)
}

const tipColumns = `id, trip_id, rider_id, driver_id, currency_code, amount, idempotency_key, created_at`

func scanTip(row pgx.Row) (tips.Tip, error) {
	var t tips.Tip

	err := row.Scan(&t.ID, &t.TripID, &t.RiderID, &t.DriverID, &t.CurrencyCode, &t.Amount, &t.IdempotencyKey, &t.CreatedAt)

	return t, err
}

func (s *TipStore) FindByKey(ctx context.Context, riderID, key string) (tips.Tip, bool, error) {
	t, err := scanTip(s.wallets.pool.QueryRow(
		ctx,
		`SELECT `+tipColumns+` FROM trip_tips WHERE rider_id = $1 AND idempotency_key = $2`,
		riderID, key,
	))

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return tips.Tip{}, false, nil
	case err != nil:
		return tips.Tip{}, false, fmt.Errorf("select tip: %w", err)
	}

	return t, true, nil
}

func (s *TipStore) Tip(ctx context.Context, record tips.Record, notBefore time.Time) (tips.Tip, wallet.Wallet, error) {
	config, err := s.wallets.GetActiveConfig(ctx)
	if err != nil {
		return tips.Tip{}, wallet.Wallet{}, err
	}

	tx, err := s.wallets.pool.Begin(ctx)
	if err != nil {
		return tips.Tip{}, wallet.Wallet{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var (
		riderID, driverID, currency, kind string
		settledAt                         time.Time
	)

	err = tx.QueryRow(
		ctx,
		`SELECT rider_id, driver_id, currency_code, kind, created_at FROM trip_settlements WHERE trip_id = $1`,
		record.TripID,
	).Scan(&riderID, &driverID, &currency, &kind, &settledAt)

	switch {
	case errors.Is(err, pgx.ErrNoRows), err == nil && riderID != record.RiderID:
		return tips.Tip{}, wallet.Wallet{}, tips.ErrTripNotFound
	case err != nil:
		return tips.Tip{}, wallet.Wallet{}, fmt.Errorf("select settlement: %w", err)
	case wallet.SettlementKind(kind) != wallet.SettlementTrip, settledAt.Before(notBefore):
		return tips.Tip{}, wallet.Wallet{}, tips.ErrNotTippable
	}

	// The rider's wallet, then the driver's: the order a trip settles in.
	rider, err := ensureWalletTx(ctx, tx, wallet.OwnerRider, riderID, currency)
	if err != nil {
		return tips.Tip{}, wallet.Wallet{}, err
	}

	driver, err := ensureWalletTx(ctx, tx, wallet.OwnerDriver, driverID, currency)
	if err != nil {
		return tips.Tip{}, wallet.Wallet{}, err
	}

	var tipped bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM trip_tips WHERE trip_id = $1)`, record.TripID).Scan(&tipped); err != nil {
		return tips.Tip{}, wallet.Wallet{}, fmt.Errorf("look for an earlier tip: %w", err)
	}

	if tipped {
		return tips.Tip{}, wallet.Wallet{}, tips.ErrAlreadyTipped
	}

	riderAfter, riderRow, err := applyMovementTx(ctx, tx, rider, wallet.MovementInput{
		OwnerType: wallet.OwnerRider, OwnerID: riderID, Type: wallet.TxTip,
		Amount: record.Amount.Neg(), TripID: record.TripID, Description: "Tip for your driver",
	})
	if err != nil {
		return tips.Tip{}, wallet.Wallet{}, err
	}

	floor := config.SuspensionFloor()

	_, driverRow, err := applyMovementTx(ctx, tx, driver, wallet.MovementInput{
		OwnerType: wallet.OwnerDriver, OwnerID: driverID, Type: wallet.TxTip,
		Amount: record.Amount, TripID: record.TripID, Description: "Tip from your rider", SuspensionFloor: &floor,
	})
	if err != nil {
		return tips.Tip{}, wallet.Wallet{}, err
	}

	given, err := scanTip(tx.QueryRow(
		ctx,
		`INSERT INTO trip_tips
		    (trip_id, rider_id, driver_id, currency_code, amount, idempotency_key, rider_transaction_id, driver_transaction_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING `+tipColumns,
		record.TripID, riderID, driverID, currency, record.Amount, record.IdempotencyKey, riderRow.ID, driverRow.ID,
	))
	if err != nil {
		switch {
		case isUniqueViolation(err, "trip_tips_rider_key_unique"):
			return tips.Tip{}, wallet.Wallet{}, wallet.ErrDuplicateRequest
		case isUniqueViolation(err, "trip_tips_trip_unique"):
			return tips.Tip{}, wallet.Wallet{}, tips.ErrAlreadyTipped
		}

		return tips.Tip{}, wallet.Wallet{}, fmt.Errorf("insert tip: %w", err)
	}

	payload, err := json.Marshal(map[string]any{
		"tip_id":        given.ID,
		"trip_id":       given.TripID,
		"rider_id":      given.RiderID,
		"driver_id":     given.DriverID,
		"amount":        given.Amount.String(),
		"currency_code": given.CurrencyCode,
	})
	if err != nil {
		return tips.Tip{}, wallet.Wallet{}, fmt.Errorf("marshal wallet.tip_received payload: %w", err)
	}

	if _, err := tx.Exec(
		ctx,
		`INSERT INTO outbox_events
		    (aggregate_type, aggregate_id, event_type, schema_version,
		     payload, occurred_at, available_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $6)`,
		"tip", given.ID, "wallet.tip_received", schemaVersion, payload, time.Now().UTC(),
	); err != nil {
		return tips.Tip{}, wallet.Wallet{}, fmt.Errorf("insert wallet.tip_received outbox event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return tips.Tip{}, wallet.Wallet{}, fmt.Errorf("commit transaction: %w", err)
	}

	return given, riderAfter, nil
}

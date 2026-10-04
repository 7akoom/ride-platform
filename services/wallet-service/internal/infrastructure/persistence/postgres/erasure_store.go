package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/erasure"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
	"github.com/jackc/pgx/v5"
)

// ForfeitDescription is written on the ledger row that empties a deleted
// account's wallet.
const ForfeitDescription = "Account deleted: balance forfeited"

// ErasureStore closes a deleted account's wallets (erasure.Eraser). The
// ledger stays, as the company's accounts need it. A positive balance (the
// person accepted losing it) is debited with an adjustment so the books
// balance, and the wallets are blocked. Phones kept on transfers and money
// requests, and payout destinations, are cleared; requests still open are
// cancelled.
type ErasureStore struct {
	wallets *WalletRepository
}

func NewErasureStore(wallets *WalletRepository) *ErasureStore {
	if wallets == nil {
		panic("wallet repository is required")
	}

	return &ErasureStore{wallets: wallets}
}

func (s *ErasureStore) Requested(context.Context, erasure.Account) error { return nil }

func (s *ErasureStore) Erase(ctx context.Context, account erasure.Account) error {
	config, err := s.wallets.GetActiveConfig(ctx)
	if err != nil {
		return err
	}

	tx, err := s.wallets.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin wallet erasure: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	owners := map[wallet.OwnerType]string{wallet.OwnerRider: account.RiderID, wallet.OwnerDriver: account.DriverID}

	for _, ownerType := range []wallet.OwnerType{wallet.OwnerRider, wallet.OwnerDriver} {
		ownerID := owners[ownerType]
		if ownerID == "" {
			continue
		}

		if err := closeWalletTx(ctx, tx, config, ownerType, ownerID); err != nil {
			return err
		}
	}

	var statements []struct{ what, sql, arg string }

	if account.RiderID != "" {
		statements = append(statements,
			struct{ what, sql, arg string }{"money requests", `UPDATE money_requests
				SET status = 'cancelled', closed_at = CURRENT_TIMESTAMP
				WHERE status = 'pending' AND (requester_rider_id = $1 OR payer_rider_id = $1)`, account.RiderID},
			struct{ what, sql, arg string }{"requester phones", `UPDATE money_requests SET requester_phone = ''
				WHERE requester_rider_id = $1 AND requester_phone <> ''`, account.RiderID},
			struct{ what, sql, arg string }{"payer phones", `UPDATE money_requests SET payer_phone = ''
				WHERE payer_rider_id = $1 AND payer_phone <> ''`, account.RiderID},
			struct{ what, sql, arg string }{"sender phones", `UPDATE wallet_transfers SET sender_phone = ''
				WHERE sender_rider_id = $1 AND sender_phone <> ''`, account.RiderID},
			struct{ what, sql, arg string }{"recipient phones", `UPDATE wallet_transfers SET recipient_phone = ''
				WHERE recipient_rider_id = $1 AND recipient_phone <> ''`, account.RiderID},
		)
	}

	if account.DriverID != "" {
		statements = append(statements,
			struct{ what, sql, arg string }{"payout destinations", `UPDATE payout_requests SET destination = ''
				WHERE driver_id = $1 AND destination <> ''`, account.DriverID},
		)
	}

	for _, st := range statements {
		if _, err := tx.Exec(ctx, st.sql, st.arg); err != nil {
			return fmt.Errorf("erase %s: %w", st.what, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit wallet erasure: %w", err)
	}

	return nil
}

func closeWalletTx(ctx context.Context, tx pgx.Tx, config wallet.Config, ownerType wallet.OwnerType, ownerID string) error {
	target, err := scanWallet(tx.QueryRow(ctx,
		walletSelectSQL+` WHERE owner_type = $1 AND owner_id = $2 FOR UPDATE`, string(ownerType), ownerID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("lock a deleted account's wallet: %w", err)
	}

	if target.Balance.IsPositive() {
		if _, _, err := applyMovementTx(ctx, tx, target, movementFor(
			config, ownerType, ownerID, wallet.TxAdjustment, target.Balance.Neg(), "", ForfeitDescription,
		)); err != nil {
			return fmt.Errorf("empty a deleted account's wallet: %w", err)
		}
	}

	if _, err := tx.Exec(ctx,
		`UPDATE wallets SET blocked = true, updated_at = CURRENT_TIMESTAMP WHERE owner_type = $1 AND owner_id = $2`,
		string(ownerType), ownerID); err != nil {
		return fmt.Errorf("block a deleted account's wallet: %w", err)
	}

	return nil
}

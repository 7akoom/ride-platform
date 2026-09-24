package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/walletpin"
)

// WalletPinStore keeps wallet PINs and answers the directory questions
// (who signs in with this phone, and with which phone this identity does).
type WalletPinStore struct {
	pool *pgxpool.Pool
}

var (
	_ walletpin.Repository = (*WalletPinStore)(nil)
	_ walletpin.Directory  = (*WalletPinStore)(nil)
)

func NewWalletPinStore(pool *pgxpool.Pool) *WalletPinStore {
	if pool == nil {
		panic("PostgreSQL pool is required")
	}

	return &WalletPinStore{pool: pool}
}

const walletPinColumns = `identity_id::text, pin_hash, failed_attempts, lockouts, locked_until, set_at`

func scanWalletPin(row pgx.Row) (walletpin.Record, error) {
	var (
		record           walletpin.Record
		failed, lockouts int16
	)

	if err := row.Scan(
		&record.IdentityID,
		&record.PINHash,
		&failed,
		&lockouts,
		&record.LockedUntil,
		&record.SetAt,
	); err != nil {
		return walletpin.Record{}, err
	}

	record.FailedAttempts, record.Lockouts = int(failed), int(lockouts)

	return record, nil
}

func (s *WalletPinStore) Find(ctx context.Context, identityID string) (walletpin.Record, bool, error) {
	record, err := scanWalletPin(s.pool.QueryRow(
		ctx,
		`SELECT `+walletPinColumns+` FROM wallet_pins WHERE identity_id = $1::uuid`,
		identityID,
	))

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return walletpin.Record{}, false, nil
	case err != nil:
		return walletpin.Record{}, false, fmt.Errorf("select wallet PIN: %w", err)
	}

	return record, true, nil
}

// Update locks the identity's row before deciding. For an identity without a
// PIN the identity row itself is locked (FOR NO KEY UPDATE: signing in,
// which only references the identity, is not held up), so two first PINs
// cannot race.
func (s *WalletPinStore) Update(
	ctx context.Context,
	identityID string,
	decide func(current walletpin.Record, found bool) walletpin.Change,
) (walletpin.Record, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return walletpin.Record{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var exists bool
	if err := tx.QueryRow(
		ctx,
		`SELECT true FROM identities WHERE id = $1::uuid FOR NO KEY UPDATE`,
		identityID,
	).Scan(&exists); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return walletpin.Record{}, walletpin.ErrIdentityRequired
		}

		return walletpin.Record{}, fmt.Errorf("lock identity: %w", err)
	}

	current, err := scanWalletPin(tx.QueryRow(
		ctx,
		`SELECT `+walletPinColumns+` FROM wallet_pins WHERE identity_id = $1::uuid FOR UPDATE`,
		identityID,
	))

	found := true

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		found = false
	case err != nil:
		return walletpin.Record{}, fmt.Errorf("lock wallet PIN: %w", err)
	}

	change := decide(current, found)
	if !change.Save {
		return current, change.Err
	}

	record := change.Record

	stored, err := scanWalletPin(tx.QueryRow(
		ctx,
		`INSERT INTO wallet_pins (identity_id, pin_hash, failed_attempts, lockouts, locked_until, set_at)
		 VALUES ($1::uuid, $2, $3, $4, $5, $6)
		 ON CONFLICT (identity_id) DO UPDATE
		 SET pin_hash = EXCLUDED.pin_hash,
		     failed_attempts = EXCLUDED.failed_attempts,
		     lockouts = EXCLUDED.lockouts,
		     locked_until = EXCLUDED.locked_until,
		     set_at = EXCLUDED.set_at,
		     updated_at = CURRENT_TIMESTAMP
		 RETURNING `+walletPinColumns,
		identityID,
		record.PINHash,
		record.FailedAttempts,
		record.Lockouts,
		record.LockedUntil,
		record.SetAt,
	))
	if err != nil {
		return walletpin.Record{}, fmt.Errorf("save wallet PIN: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return walletpin.Record{}, fmt.Errorf("commit transaction: %w", err)
	}

	return stored, change.Err
}

func (s *WalletPinStore) SessionStartedAt(
	ctx context.Context,
	identityID, sessionID string,
) (time.Time, bool, error) {
	if strings.TrimSpace(sessionID) == "" {
		return time.Time{}, false, nil
	}

	var started time.Time

	err := s.pool.QueryRow(
		ctx,
		`SELECT created_at
		 FROM auth_sessions
		 WHERE id::text = $2 AND identity_id = $1::uuid
		   AND revoked_at IS NULL AND expires_at > CURRENT_TIMESTAMP`,
		identityID, sessionID,
	).Scan(&started)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return time.Time{}, false, nil
	case err != nil:
		return time.Time{}, false, fmt.Errorf("select session: %w", err)
	}

	return started, true, nil
}

func (s *WalletPinStore) FindActiveByPhone(ctx context.Context, phoneNumber string) (string, bool, error) {
	var identityID string

	err := s.pool.QueryRow(
		ctx,
		`SELECT i.id::text
		 FROM identity_identifiers ii
		 JOIN identities i ON i.id = ii.identity_id
		 WHERE ii.identifier_type = 'phone' AND ii.normalized_value = $1 AND i.status = 'active'`,
		phoneNumber,
	).Scan(&identityID)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return "", false, nil
	case err != nil:
		return "", false, fmt.Errorf("find identity by phone: %w", err)
	}

	return identityID, true, nil
}

func (s *WalletPinStore) PhoneOf(ctx context.Context, identityID string) (string, error) {
	var phone string

	err := s.pool.QueryRow(
		ctx,
		`SELECT normalized_value
		 FROM identity_identifiers
		 WHERE identity_id = $1::uuid AND identifier_type = 'phone'
		 ORDER BY created_at, id
		 LIMIT 1`,
		identityID,
	).Scan(&phone)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return "", nil
	case err != nil:
		return "", fmt.Errorf("find the identity's phone: %w", err)
	}

	return phone, nil
}

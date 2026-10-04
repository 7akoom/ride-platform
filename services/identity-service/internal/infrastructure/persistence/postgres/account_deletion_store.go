package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	"github.com/7akoom/ride-platform/services/identity-service/internal/application/deletion"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AccountDeletionStore keeps account deletions (deletion.Store).
type AccountDeletionStore struct {
	pool *pgxpool.Pool
}

func NewAccountDeletionStore(pool *pgxpool.Pool) *AccountDeletionStore {
	if pool == nil {
		panic("database pool is required")
	}

	return &AccountDeletionStore{pool: pool}
}

type accountDeletionRequestedPayload struct {
	IdentityID string    `json:"identity_id"`
	RiderID    string    `json:"rider_id"`
	DriverID   string    `json:"driver_id"`
	PurgeAfter time.Time `json:"purge_after"`
}

type accountDeletedPayload struct {
	IdentityID string `json:"identity_id"`
	RiderID    string `json:"rider_id"`
	DriverID   string `json:"driver_id"`
}

const accountDeletionColumns = `identity_id::text, status, COALESCE(rider_id::text, ''), COALESCE(driver_id::text, ''),
	requested_at, purge_after`

func scanAccountDeletion(row pgx.Row) (deletion.Deletion, error) {
	var (
		d      deletion.Deletion
		status string
	)

	if err := row.Scan(&d.IdentityID, &status, &d.RiderID, &d.DriverID, &d.RequestedAt, &d.PurgeAfter); err != nil {
		return deletion.Deletion{}, err
	}

	d.Status = deletion.Status(status)

	return d, nil
}

func (s *AccountDeletionStore) Find(ctx context.Context, identityID string) (deletion.Deletion, bool, error) {
	if !isUUIDText(identityID) {
		return deletion.Deletion{}, false, nil
	}

	d, err := scanAccountDeletion(s.pool.QueryRow(ctx,
		`SELECT `+accountDeletionColumns+` FROM account_deletions WHERE identity_id = $1`, identityID))
	if errors.Is(err, pgx.ErrNoRows) {
		return deletion.Deletion{}, false, nil
	}

	if err != nil {
		return deletion.Deletion{}, false, fmt.Errorf("read account deletion: %w", err)
	}

	return d, true, nil
}

func (s *AccountDeletionStore) CreateChallenge(ctx context.Context, challenge auth.OTPChallenge) error {
	if challenge.Purpose != auth.OTPPurposeDeleteAccount || challenge.TargetIdentityID == nil {
		return errors.New("not an account deletion challenge")
	}

	identityID := strings.TrimSpace(*challenge.TargetIdentityID)

	switch {
	case strings.TrimSpace(challenge.ID) == "":
		return errors.New("OTP challenge ID cannot be blank")
	case strings.TrimSpace(challenge.CodeHash) == "":
		return errors.New("OTP challenge code hash cannot be blank")
	case challenge.ExpiresAt.IsZero():
		return errors.New("OTP challenge expiration cannot be zero")
	}

	identifier, err := auth.NewIdentifier(challenge.Identifier.Type, challenge.Identifier.Value)
	if err != nil {
		return err
	}

	tenantHint, err := normalizeChallengeTenantHint(challenge.TenantHint)
	if err != nil {
		return err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin deletion challenge transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var owned bool

	// The code goes to one of the identity's own sign-in methods only.
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (
		     SELECT 1 FROM identity_identifiers
		     WHERE identity_id = $1::uuid AND identifier_type = $2 AND normalized_value = $3
		 )`, identityID, string(identifier.Type), identifier.Value).Scan(&owned); err != nil {
		return fmt.Errorf("check the deletion code's sign-in method: %w", err)
	}

	if !owned {
		return auth.ErrIdentifierNotLinked
	}

	if _, err := tx.Exec(ctx,
		`UPDATE otp_challenges SET cancelled_at = statement_timestamp()
		 WHERE purpose = 'delete_account' AND target_identity_id = $1::uuid
		   AND verified_at IS NULL AND cancelled_at IS NULL AND expires_at > statement_timestamp()`,
		identityID); err != nil {
		return fmt.Errorf("cancel earlier deletion challenges: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO otp_challenges
		     (id, identifier_type, normalized_value, purpose, target_identity_id, tenant_hint, code_hash, expires_at)
		 VALUES ($1, $2, $3, 'delete_account', $4::uuid, $5, $6, $7)`,
		challenge.ID, string(identifier.Type), identifier.Value, identityID, tenantHint,
		challenge.CodeHash, challenge.ExpiresAt.UTC()); err != nil {
		return fmt.Errorf("insert the deletion challenge: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit deletion challenge: %w", err)
	}

	return nil
}

func (s *AccountDeletionStore) Confirm(ctx context.Context, input deletion.ConfirmInput) (deletion.Deletion, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return deletion.Deletion{}, fmt.Errorf("begin account deletion transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var (
		purpose     string
		target      string
		verifiedAt  *time.Time
		cancelledAt *time.Time
		expiresAt   time.Time
	)

	err = tx.QueryRow(ctx,
		`SELECT purpose, COALESCE(target_identity_id::text, ''), verified_at, cancelled_at, expires_at
		 FROM otp_challenges WHERE id = $1 FOR UPDATE`, input.ChallengeID).
		Scan(&purpose, &target, &verifiedAt, &cancelledAt, &expiresAt)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return deletion.Deletion{}, auth.ErrChallengeNotFound
	case err != nil:
		return deletion.Deletion{}, fmt.Errorf("lock the deletion challenge: %w", err)
	case purpose != string(auth.OTPPurposeDeleteAccount):
		return deletion.Deletion{}, auth.ErrOTPPurposeMismatch
	case target != input.IdentityID:
		return deletion.Deletion{}, auth.ErrOTPChallengeTargetMismatch
	case verifiedAt != nil:
		return deletion.Deletion{}, auth.ErrChallengeUsed
	case cancelledAt != nil:
		return deletion.Deletion{}, auth.ErrChallengeCancelled
	case !input.VerifiedAt.Before(expiresAt):
		return deletion.Deletion{}, auth.ErrChallengeExpired
	}

	if _, err := tx.Exec(ctx, `UPDATE otp_challenges SET verified_at = $2 WHERE id = $1`,
		input.ChallengeID, input.VerifiedAt); err != nil {
		return deletion.Deletion{}, fmt.Errorf("mark the deletion challenge used: %w", err)
	}

	var status string

	err = tx.QueryRow(ctx, `SELECT status FROM identities WHERE id = $1::uuid FOR UPDATE`, input.IdentityID).Scan(&status)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return deletion.Deletion{}, auth.ErrIdentityNotFound
	case err != nil:
		return deletion.Deletion{}, fmt.Errorf("lock identity for deletion: %w", err)
	case status != string(auth.IdentityStatusActive):
		return deletion.Deletion{}, deletion.ErrNotActive
	}

	d, err := scanAccountDeletion(tx.QueryRow(ctx,
		`INSERT INTO account_deletions
		     (identity_id, status, rider_id, driver_id, balance_loss_accepted, requested_at, purge_after, updated_at)
		 VALUES ($1::uuid, 'pending', NULLIF($2, '')::uuid, NULLIF($3, '')::uuid, $4, $5, $6, $5)
		 ON CONFLICT (identity_id) DO UPDATE SET
		     status = 'pending', rider_id = EXCLUDED.rider_id, driver_id = EXCLUDED.driver_id,
		     balance_loss_accepted = EXCLUDED.balance_loss_accepted,
		     requested_at = EXCLUDED.requested_at, purge_after = EXCLUDED.purge_after,
		     cancelled_at = NULL, completed_at = NULL, next_attempt_at = NULL, attempts = 0, last_error = '',
		     updated_at = EXCLUDED.updated_at
		 WHERE account_deletions.status = 'cancelled'
		 RETURNING `+accountDeletionColumns,
		input.IdentityID, input.RiderID, input.DriverID, input.BalanceLossAccepted, input.VerifiedAt, input.PurgeAfter))
	if errors.Is(err, pgx.ErrNoRows) {
		// The row exists and is pending (or completed): nothing to start.
		return deletion.Deletion{}, deletion.ErrAlreadyPending
	}

	if err != nil {
		return deletion.Deletion{}, fmt.Errorf("record account deletion: %w", err)
	}

	if err := revokeIdentitySessionsInTransaction(ctx, tx, input.IdentityID, input.VerifiedAt); err != nil {
		return deletion.Deletion{}, err
	}

	if err := insertIdentityOutboxEventInTransaction(ctx, tx, input.IdentityID,
		auth.IdentityDomainEventDeletionRequested, auth.IdentityDomainEventSchemaVersion,
		accountDeletionRequestedPayload{
			IdentityID: input.IdentityID, RiderID: input.RiderID, DriverID: input.DriverID, PurgeAfter: input.PurgeAfter.UTC(),
		}, input.VerifiedAt); err != nil {
		return deletion.Deletion{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return deletion.Deletion{}, fmt.Errorf("commit account deletion: %w", err)
	}

	return d, nil
}

func (s *AccountDeletionStore) Due(ctx context.Context, now time.Time, limit int) ([]deletion.Deletion, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+accountDeletionColumns+` FROM account_deletions
		 WHERE status = 'pending' AND purge_after <= $1 AND (next_attempt_at IS NULL OR next_attempt_at <= $1)
		 ORDER BY purge_after, identity_id
		 LIMIT $2`, now, limit)
	if err != nil {
		return nil, fmt.Errorf("list due account deletions: %w", err)
	}
	defer rows.Close()

	var out []deletion.Deletion

	for rows.Next() {
		d, err := scanAccountDeletion(rows)
		if err != nil {
			return nil, fmt.Errorf("scan account deletion: %w", err)
		}

		out = append(out, d)
	}

	return out, rows.Err()
}

func (s *AccountDeletionStore) Postpone(ctx context.Context, identityID string, until time.Time, reason string) error {
	if len(reason) > 300 {
		reason = reason[:300]
	}

	if _, err := s.pool.Exec(ctx,
		`UPDATE account_deletions
		 SET next_attempt_at = $2, attempts = attempts + 1, last_error = $3, updated_at = CURRENT_TIMESTAMP
		 WHERE identity_id = $1::uuid AND status = 'pending'`,
		identityID, until, strings.ToValidUTF8(reason, "")); err != nil {
		return fmt.Errorf("postpone account deletion: %w", err)
	}

	return nil
}

// eraseIdentityStatements remove everything that says who the identity was:
// sign-in methods, codes sent to them, sessions (devices, addresses), the PIN.
// The identities row stays, disabled, as the anonymous id the other services'
// records keep pointing at.
var eraseIdentityStatements = []struct{ what, sql string }{
	{"delivery attempts", `DELETE FROM otp_delivery_attempts WHERE challenge_id IN (
		SELECT c.id FROM otp_challenges c
		WHERE c.target_identity_id = $1::uuid
		   OR (c.identifier_type, c.normalized_value) IN (
		       SELECT identifier_type, normalized_value FROM identity_identifiers WHERE identity_id = $1::uuid))`},
	{"codes", `DELETE FROM otp_challenges c
		WHERE c.target_identity_id = $1::uuid
		   OR (c.identifier_type, c.normalized_value) IN (
		       SELECT identifier_type, normalized_value FROM identity_identifiers WHERE identity_id = $1::uuid)`},
	{"code requests", `DELETE FROM otp_request_events e
		WHERE e.target_identity_id = $1::uuid
		   OR (e.identifier_type, e.normalized_value) IN (
		       SELECT identifier_type, normalized_value FROM identity_identifiers WHERE identity_id = $1::uuid)`},
	{"unlink operations", `DELETE FROM identifier_unlink_operations WHERE identity_id = $1::uuid`},
	{"sessions", `DELETE FROM auth_sessions WHERE identity_id = $1::uuid`},
	{"wallet PIN", `DELETE FROM wallet_pins WHERE identity_id = $1::uuid`},
	{"data exports", `DELETE FROM data_exports WHERE identity_id = $1::uuid`},
	{"sign-in methods", `DELETE FROM identity_identifiers WHERE identity_id = $1::uuid`},
}

func (s *AccountDeletionStore) Complete(ctx context.Context, input deletion.CompleteInput) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin account erase transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var status string

	err = tx.QueryRow(ctx,
		`SELECT status FROM account_deletions WHERE identity_id = $1::uuid FOR UPDATE`, input.IdentityID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && status != string(deletion.StatusPending)) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("lock account deletion: %w", err)
	}

	if _, err := tx.Exec(ctx, `SELECT 1 FROM identities WHERE id = $1::uuid FOR UPDATE`, input.IdentityID); err != nil {
		return false, fmt.Errorf("lock identity for erasing: %w", err)
	}

	for _, statement := range eraseIdentityStatements {
		if _, err := tx.Exec(ctx, statement.sql, input.IdentityID); err != nil {
			return false, fmt.Errorf("erase %s: %w", statement.what, err)
		}
	}

	if _, err := tx.Exec(ctx,
		`UPDATE identities SET status = 'disabled', updated_at = $2 WHERE id = $1::uuid`,
		input.IdentityID, input.CompletedAt); err != nil {
		return false, fmt.Errorf("disable erased identity: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`UPDATE account_deletions
		 SET status = 'completed', completed_at = $2, rider_id = NULLIF($3, '')::uuid, driver_id = NULLIF($4, '')::uuid,
		     next_attempt_at = NULL, last_error = '', updated_at = $2
		 WHERE identity_id = $1::uuid`,
		input.IdentityID, input.CompletedAt, input.RiderID, input.DriverID); err != nil {
		return false, fmt.Errorf("complete account deletion: %w", err)
	}

	if err := insertIdentityOutboxEventInTransaction(ctx, tx, input.IdentityID,
		auth.IdentityDomainEventDeleted, auth.IdentityDomainEventSchemaVersion,
		accountDeletedPayload{IdentityID: input.IdentityID, RiderID: input.RiderID, DriverID: input.DriverID},
		input.CompletedAt); err != nil {
		return false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit account erase: %w", err)
	}

	return true, nil
}

func isUUIDText(value string) bool {
	if len(value) != 36 {
		return false
	}

	for i, r := range value {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
				return false
			}
		}
	}

	return true
}

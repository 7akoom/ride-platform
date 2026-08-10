package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type RefreshTokenRotationStore struct {
	pool *pgxpool.Pool
}

var _ auth.RefreshTokenRotationStore = (*RefreshTokenRotationStore)(nil)

func NewRefreshTokenRotationStore(
	pool *pgxpool.Pool,
) *RefreshTokenRotationStore {
	return &RefreshTokenRotationStore{
		pool: pool,
	}
}

func (s *RefreshTokenRotationStore) Inspect(
	ctx context.Context,
	currentTokenHash string,
	now time.Time,
) (auth.RefreshTokenContext, error) {
	if currentTokenHash == "" {
		return auth.RefreshTokenContext{},
			auth.ErrInvalidRefreshToken
	}

	now = now.UTC()

	const query = `
		SELECT
			i.id::text,
			i.status,
			s.id::text,
			s.expires_at,
			s.revoked_at,
			rt.expires_at,
			rt.used_at,
			rt.revoked_at
		FROM refresh_tokens AS rt
		INNER JOIN auth_sessions AS s
			ON s.id = rt.session_id
		INNER JOIN identities AS i
			ON i.id = s.identity_id
		WHERE rt.token_hash = $1
	`

	var refreshContext auth.RefreshTokenContext
	var identityStatus string
	var sessionRevokedAt *time.Time
	var refreshTokenExpiresAt time.Time
	var refreshTokenUsedAt *time.Time
	var refreshTokenRevokedAt *time.Time

	err := s.pool.QueryRow(
		ctx,
		query,
		currentTokenHash,
	).Scan(
		&refreshContext.IdentityID,
		&identityStatus,
		&refreshContext.SessionID,
		&refreshContext.SessionExpiresAt,
		&sessionRevokedAt,
		&refreshTokenExpiresAt,
		&refreshTokenUsedAt,
		&refreshTokenRevokedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return auth.RefreshTokenContext{},
			auth.ErrInvalidRefreshToken
	}

	if err != nil {
		return auth.RefreshTokenContext{}, fmt.Errorf(
			"query refresh token context: %w",
			err,
		)
	}

	if sessionRevokedAt != nil {
		return auth.RefreshTokenContext{},
			auth.ErrSessionRevoked
	}

	if !now.Before(refreshContext.SessionExpiresAt) {
		return auth.RefreshTokenContext{},
			auth.ErrSessionExpired
	}

	if identityStatus != "active" {
		return auth.RefreshTokenContext{},
			auth.ErrIdentityInactive
	}

	if refreshTokenRevokedAt != nil {
		return auth.RefreshTokenContext{},
			auth.ErrRefreshTokenRevoked
	}

	if !now.Before(refreshTokenExpiresAt) {
		return auth.RefreshTokenContext{},
			auth.ErrRefreshTokenExpired
	}

	if refreshTokenUsedAt != nil {
		if err := s.revokeSession(
			ctx,
			refreshContext.SessionID,
			now,
		); err != nil {
			return auth.RefreshTokenContext{}, fmt.Errorf(
				"revoke session after refresh token reuse: %w",
				err,
			)
		}

		return auth.RefreshTokenContext{},
			auth.ErrRefreshTokenReused
	}

	return refreshContext, nil
}

func (s *RefreshTokenRotationStore) Rotate(
	ctx context.Context,
	input auth.RefreshTokenRotationInput,
) error {
	if input.CurrentTokenHash == "" {
		return auth.ErrInvalidRefreshToken
	}

	if input.ReplacementTokenHash == "" {
		return errors.New(
			"replacement refresh token hash cannot be empty",
		)
	}

	if input.CurrentTokenHash ==
		input.ReplacementTokenHash {
		return errors.New(
			"replacement refresh token hash must differ from current token hash",
		)
	}

	input.RotatedAt = input.RotatedAt.UTC()
	input.ReplacementExpiresAt =
		input.ReplacementExpiresAt.UTC()

	if !input.ReplacementExpiresAt.After(
		input.RotatedAt,
	) {
		return errors.New(
			"replacement refresh token expiration must be after rotation time",
		)
	}

	tx, err := s.pool.BeginTx(
		ctx,
		pgx.TxOptions{},
	)
	if err != nil {
		return fmt.Errorf(
			"begin refresh token rotation transaction: %w",
			err,
		)
	}

	defer func() {
		_ = tx.Rollback(ctx)
	}()

	const selectQuery = `
		SELECT
			rt.id::text,
			rt.session_id::text,
			rt.expires_at,
			rt.used_at,
			rt.revoked_at,
			s.expires_at,
			s.revoked_at,
			i.status
		FROM refresh_tokens AS rt
		INNER JOIN auth_sessions AS s
			ON s.id = rt.session_id
		INNER JOIN identities AS i
			ON i.id = s.identity_id
		WHERE rt.token_hash = $1
		FOR UPDATE OF rt, s
	`

	var currentRefreshTokenID string
	var sessionID string
	var refreshTokenExpiresAt time.Time
	var refreshTokenUsedAt *time.Time
	var refreshTokenRevokedAt *time.Time
	var sessionExpiresAt time.Time
	var sessionRevokedAt *time.Time
	var identityStatus string

	err = tx.QueryRow(
		ctx,
		selectQuery,
		input.CurrentTokenHash,
	).Scan(
		&currentRefreshTokenID,
		&sessionID,
		&refreshTokenExpiresAt,
		&refreshTokenUsedAt,
		&refreshTokenRevokedAt,
		&sessionExpiresAt,
		&sessionRevokedAt,
		&identityStatus,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return auth.ErrInvalidRefreshToken
	}

	if err != nil {
		return fmt.Errorf(
			"lock current refresh token: %w",
			err,
		)
	}

	if sessionRevokedAt != nil {
		return auth.ErrSessionRevoked
	}

	if !input.RotatedAt.Before(sessionExpiresAt) {
		return auth.ErrSessionExpired
	}

	if identityStatus != "active" {
		return auth.ErrIdentityInactive
	}

	if refreshTokenRevokedAt != nil {
		return auth.ErrRefreshTokenRevoked
	}

	if !input.RotatedAt.Before(
		refreshTokenExpiresAt,
	) {
		return auth.ErrRefreshTokenExpired
	}

	if refreshTokenUsedAt != nil {
		if err := revokeSessionInTransaction(
			ctx,
			tx,
			sessionID,
			input.RotatedAt,
		); err != nil {
			return fmt.Errorf(
				"revoke session after refresh token reuse: %w",
				err,
			)
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf(
				"commit session revocation after refresh token reuse: %w",
				err,
			)
		}

		return auth.ErrRefreshTokenReused
	}

	if input.ReplacementExpiresAt.After(
		sessionExpiresAt,
	) {
		return errors.New(
			"replacement refresh token cannot expire after session",
		)
	}

	const insertReplacementQuery = `
		INSERT INTO refresh_tokens (
			session_id,
			token_hash,
			expires_at,
			created_at
		)
		VALUES (
			$1::uuid,
			$2,
			$3,
			$4
		)
		RETURNING id::text
	`

	var replacementRefreshTokenID string

	err = tx.QueryRow(
		ctx,
		insertReplacementQuery,
		sessionID,
		input.ReplacementTokenHash,
		input.ReplacementExpiresAt,
		input.RotatedAt,
	).Scan(
		&replacementRefreshTokenID,
	)
	if err != nil {
		return fmt.Errorf(
			"insert replacement refresh token: %w",
			err,
		)
	}

	const consumeCurrentTokenQuery = `
		UPDATE refresh_tokens
		SET
			used_at = $1,
			replaced_by_token_id = $2::uuid
		WHERE id = $3::uuid
		  AND used_at IS NULL
		  AND revoked_at IS NULL
	`

	commandTag, err := tx.Exec(
		ctx,
		consumeCurrentTokenQuery,
		input.RotatedAt,
		replacementRefreshTokenID,
		currentRefreshTokenID,
	)
	if err != nil {
		return fmt.Errorf(
			"consume current refresh token: %w",
			err,
		)
	}

	if commandTag.RowsAffected() != 1 {
		return errors.New(
			"current refresh token could not be consumed",
		)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf(
			"commit refresh token rotation: %w",
			err,
		)
	}

	return nil
}

func (s *RefreshTokenRotationStore) revokeSession(
	ctx context.Context,
	sessionID string,
	revokedAt time.Time,
) error {
	tx, err := s.pool.BeginTx(
		ctx,
		pgx.TxOptions{},
	)
	if err != nil {
		return fmt.Errorf(
			"begin session revocation transaction: %w",
			err,
		)
	}

	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if err := revokeSessionInTransaction(
		ctx,
		tx,
		sessionID,
		revokedAt,
	); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf(
			"commit session revocation: %w",
			err,
		)
	}

	return nil
}

func revokeSessionInTransaction(
	ctx context.Context,
	tx pgx.Tx,
	sessionID string,
	revokedAt time.Time,
) error {
	const revokeSessionQuery = `
		UPDATE auth_sessions
		SET
			revoked_at = COALESCE(
				revoked_at,
				$1
			),
			updated_at = $1
		WHERE id = $2::uuid
	`

	if _, err := tx.Exec(
		ctx,
		revokeSessionQuery,
		revokedAt,
		sessionID,
	); err != nil {
		return fmt.Errorf(
			"revoke authentication session: %w",
			err,
		)
	}

	const revokeRefreshTokensQuery = `
		UPDATE refresh_tokens
		SET revoked_at = COALESCE(
			revoked_at,
			$1
		)
		WHERE session_id = $2::uuid
		  AND revoked_at IS NULL
	`

	if _, err := tx.Exec(
		ctx,
		revokeRefreshTokensQuery,
		revokedAt,
		sessionID,
	); err != nil {
		return fmt.Errorf(
			"revoke session refresh tokens: %w",
			err,
		)
	}

	return nil
}
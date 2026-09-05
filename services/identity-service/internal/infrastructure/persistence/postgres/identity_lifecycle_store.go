package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type IdentityLifecycleStore struct {
	pool *pgxpool.Pool
}

var _ auth.IdentityLifecycleStore = (*IdentityLifecycleStore)(nil)

func NewIdentityLifecycleStore(
	pool *pgxpool.Pool,
) *IdentityLifecycleStore {
	if pool == nil {
		panic("PostgreSQL pool is required")
	}

	return &IdentityLifecycleStore{
		pool: pool,
	}
}

func (s *IdentityLifecycleStore) Transition(
	ctx context.Context,
	input auth.IdentityLifecycleTransition,
) (auth.IdentityLifecycleTransitionResult, bool, error) {
	identityID := strings.TrimSpace(input.IdentityID)
	if identityID == "" {
		return auth.IdentityLifecycleTransitionResult{},
			false,
			errors.New("identity ID cannot be blank")
	}

	if input.TransitionedAt.IsZero() {
		return auth.IdentityLifecycleTransitionResult{},
			false,
			errors.New(
				"identity lifecycle transition time cannot be zero",
			)
	}

	targetStatus, err := auth.ParseIdentityStatus(
		string(input.TargetStatus),
	)
	if err != nil {
		return auth.IdentityLifecycleTransitionResult{},
			false,
			fmt.Errorf(
				"validate target identity status: %w",
				err,
			)
	}

	transitionedAt := input.TransitionedAt.UTC()

	tx, err := s.pool.BeginTx(
		ctx,
		pgx.TxOptions{},
	)
	if err != nil {
		return auth.IdentityLifecycleTransitionResult{},
			false,
			fmt.Errorf(
				"begin identity lifecycle transaction: %w",
				err,
			)
	}

	defer func() {
		_ = tx.Rollback(ctx)
	}()

	const lockIdentityQuery = `
		SELECT status
		FROM identities
		WHERE id = $1::uuid
		FOR UPDATE
	`

	var currentStatusValue string

	err = tx.QueryRow(
		ctx,
		lockIdentityQuery,
		identityID,
	).Scan(
		&currentStatusValue,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return auth.IdentityLifecycleTransitionResult{},
			false,
			nil
	}

	if err != nil {
		return auth.IdentityLifecycleTransitionResult{},
			false,
			fmt.Errorf(
				"lock identity for lifecycle transition: %w",
				err,
			)
	}

	currentStatus, err := auth.ParseIdentityStatus(
		currentStatusValue,
	)
	if err != nil {
		return auth.IdentityLifecycleTransitionResult{},
			false,
			fmt.Errorf(
				"parse current identity status: %w",
				err,
			)
	}

	result := auth.IdentityLifecycleTransitionResult{
		PreviousStatus: currentStatus,
		CurrentStatus:  currentStatus,
		Changed:        false,
	}

	if currentStatus == targetStatus {
		return result, true, nil
	}

	event, err := auth.NewIdentityLifecycleDomainEvent(
		identityID,
		currentStatus,
		targetStatus,
		transitionedAt,
	)
	if err != nil {
		return auth.IdentityLifecycleTransitionResult{},
			false,
			fmt.Errorf(
				"build identity lifecycle domain event: %w",
				err,
			)
	}

	const updateIdentityQuery = `
		UPDATE identities
		SET
			status = $2,
			updated_at = $3
		WHERE id = $1::uuid
	`

	commandTag, err := tx.Exec(
		ctx,
		updateIdentityQuery,
		identityID,
		string(targetStatus),
		transitionedAt,
	)
	if err != nil {
		return auth.IdentityLifecycleTransitionResult{},
			false,
			fmt.Errorf(
				"update identity lifecycle status: %w",
				err,
			)
	}

	if commandTag.RowsAffected() != 1 {
		return auth.IdentityLifecycleTransitionResult{},
			false,
			errors.New(
				"identity lifecycle transition did not update exactly one identity",
			)
	}

	if targetStatus != auth.IdentityStatusActive {
		if err := revokeIdentitySessionsInTransaction(
			ctx,
			tx,
			identityID,
			transitionedAt,
		); err != nil {
			return auth.IdentityLifecycleTransitionResult{},
				false,
				err
		}
	}

	if err := insertIdentityLifecycleOutboxEventInTransaction(
		ctx,
		tx,
		event,
	); err != nil {
		return auth.IdentityLifecycleTransitionResult{},
			false,
			err
	}

	if err := tx.Commit(ctx); err != nil {
		return auth.IdentityLifecycleTransitionResult{},
			false,
			fmt.Errorf(
				"commit identity lifecycle transaction: %w",
				err,
			)
	}

	result.CurrentStatus = targetStatus
	result.Changed = true

	return result, true, nil
}

func revokeIdentitySessionsInTransaction(
	ctx context.Context,
	tx pgx.Tx,
	identityID string,
	revokedAt time.Time,
) error {
	const revokeSessionsQuery = `
		UPDATE auth_sessions
		SET
			revoked_at = COALESCE(
				revoked_at,
				$2
			),
			updated_at = $2
		WHERE identity_id = $1::uuid
		  AND revoked_at IS NULL
	`

	if _, err := tx.Exec(
		ctx,
		revokeSessionsQuery,
		identityID,
		revokedAt,
	); err != nil {
		return fmt.Errorf(
			"revoke identity authentication sessions: %w",
			err,
		)
	}

	const revokeRefreshTokensQuery = `
		UPDATE refresh_tokens
		SET revoked_at = COALESCE(
			revoked_at,
			$2
		)
		WHERE session_id IN (
			SELECT id
			FROM auth_sessions
			WHERE identity_id = $1::uuid
		)
		  AND revoked_at IS NULL
	`

	if _, err := tx.Exec(
		ctx,
		revokeRefreshTokensQuery,
		identityID,
		revokedAt,
	); err != nil {
		return fmt.Errorf(
			"revoke identity refresh tokens: %w",
			err,
		)
	}

	return nil
}

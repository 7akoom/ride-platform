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

type OTPRequestRateLimiter struct {
	pool *pgxpool.Pool
}

var _ auth.OTPRequestRateLimiter = (*OTPRequestRateLimiter)(nil)

func NewOTPRequestRateLimiter(
	pool *pgxpool.Pool,
) *OTPRequestRateLimiter {
	if pool == nil {
		panic("PostgreSQL pool is required")
	}

	return &OTPRequestRateLimiter{
		pool: pool,
	}
}

func (r *OTPRequestRateLimiter) Allow(
	ctx context.Context,
	scope auth.OTPRequestScope,
	now time.Time,
	policy auth.OTPRequestRateLimitPolicy,
) error {
	if scope.Identifier.Type == "" {
		return errors.New("identifier type cannot be empty")
	}

	if scope.Identifier.Value == "" {
		return errors.New("identifier value cannot be empty")
	}

	if scope.Purpose == "" {
		return errors.New("OTP purpose cannot be empty")
	}

	if now.IsZero() {
		return errors.New(
			"OTP request time cannot be zero",
		)
	}

	if policy.Cooldown <= 0 {
		return errors.New(
			"OTP request cooldown must be greater than zero",
		)
	}

	if policy.Window <= 0 {
		return errors.New(
			"OTP request window must be greater than zero",
		)
	}

	if policy.MaxRequests <= 0 {
		return errors.New(
			"OTP request max requests must be greater than zero",
		)
	}

	if policy.Cooldown > policy.Window {
		return errors.New(
			"OTP request cooldown cannot exceed window",
		)
	}

	now = now.UTC()

	var targetIdentityID any

	if scope.TargetIdentityID != nil {
		targetIdentityID = *scope.TargetIdentityID
	}

	lockKey := fmt.Sprintf(
		"%s:%s:%s:%v",
		scope.Identifier.Type,
		scope.Identifier.Value,
		scope.Purpose,
		targetIdentityID,
	)

	tx, err := r.pool.BeginTx(
		ctx,
		pgx.TxOptions{},
	)
	if err != nil {
		return fmt.Errorf(
			"begin OTP rate limit transaction: %w",
			err,
		)
	}

	defer func() {
		_ = tx.Rollback(ctx)
	}()

	const lockQuery = `
		SELECT pg_advisory_xact_lock(
			hashtextextended($1, 0)
		)
	`

	if _, err := tx.Exec(
		ctx,
		lockQuery,
		lockKey,
	); err != nil {
		return fmt.Errorf(
			"lock OTP rate limit key: %w",
			err,
		)
	}

	latestRequestQuery := `
		SELECT requested_at
		FROM otp_request_events
		WHERE identifier_type = $1
		AND normalized_value = $2
		AND purpose = $3
		AND target_identity_id IS NULL
		ORDER BY requested_at DESC
		LIMIT 1
	`

	latestRequestArgs := []any{
		string(scope.Identifier.Type),
		scope.Identifier.Value,
		string(scope.Purpose),
	}

	if scope.TargetIdentityID != nil {
		latestRequestQuery = `
			SELECT requested_at
			FROM otp_request_events
			WHERE identifier_type = $1
			AND normalized_value = $2
			AND purpose = $3
			AND target_identity_id = $4::uuid
			ORDER BY requested_at DESC
			LIMIT 1
		`

		latestRequestArgs = append(
			latestRequestArgs,
			*scope.TargetIdentityID,
		)
	}

	var latestRequestedAt time.Time

	err = tx.QueryRow(
		ctx,
		latestRequestQuery,
		latestRequestArgs...,
	).Scan(
		&latestRequestedAt,
	)

	if err != nil && !errors.Is(
		err,
		pgx.ErrNoRows,
	) {
		return fmt.Errorf(
			"query latest OTP request: %w",
			err,
		)
	}

	if err == nil {
		nextAllowedAt := latestRequestedAt.Add(
			policy.Cooldown,
		)

		if now.Before(nextAllowedAt) {
			return auth.ErrOTPRequestRateLimited
		}
	}

	windowStartedAt := now.Add(
		-policy.Window,
	)

	countQuery := `
		SELECT COUNT(*)
		FROM otp_request_events
		WHERE identifier_type = $1
		AND normalized_value = $2
		AND purpose = $3
		AND target_identity_id IS NULL
		AND requested_at >= $4
	`

	countQueryArgs := []any{
		string(scope.Identifier.Type),
		scope.Identifier.Value,
		string(scope.Purpose),
		windowStartedAt,
	}

	if scope.TargetIdentityID != nil {
		countQuery = `
			SELECT COUNT(*)
			FROM otp_request_events
			WHERE identifier_type = $1
			AND normalized_value = $2
			AND purpose = $3
			AND target_identity_id = $4::uuid
			AND requested_at >= $5
		`

		countQueryArgs = []any{
			string(scope.Identifier.Type),
			scope.Identifier.Value,
			string(scope.Purpose),
			*scope.TargetIdentityID,
			windowStartedAt,
		}
	}

	var requestCount int

	if err := tx.QueryRow(
		ctx,
		countQuery,
		countQueryArgs...,
	).Scan(
		&requestCount,
	); err != nil {
		return fmt.Errorf(
			"count OTP requests in window: %w",
			err,
		)
	}

	if requestCount >= policy.MaxRequests {
		return auth.ErrOTPRequestRateLimited
	}

	const insertQuery = `
		INSERT INTO otp_request_events (
			identifier_type,
			normalized_value,
			purpose,
			target_identity_id,
			requested_at
		)
		VALUES (
			$1,
			$2,
			$3,
			$4,
			$5
		)
	`

	if _, err := tx.Exec(
		ctx,
		insertQuery,
		string(scope.Identifier.Type),
		scope.Identifier.Value,
		string(scope.Purpose),
		targetIdentityID,
		now,
	); err != nil {
		return fmt.Errorf(
			"record allowed OTP request: %w",
			err,
		)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf(
			"commit OTP rate limit transaction: %w",
			err,
		)
	}

	return nil
}

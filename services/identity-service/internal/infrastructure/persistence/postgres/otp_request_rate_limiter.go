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
	return &OTPRequestRateLimiter{
		pool: pool,
	}
}

func (r *OTPRequestRateLimiter) Allow(
	ctx context.Context,
	phoneNumber string,
	now time.Time,
	policy auth.OTPRequestRateLimitPolicy,
) error {
	if phoneNumber == "" {
		return errors.New("phone number cannot be empty")
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
		phoneNumber,
	); err != nil {
		return fmt.Errorf(
			"lock OTP rate limit key: %w",
			err,
		)
	}

	const latestRequestQuery = `
		SELECT requested_at
		FROM otp_request_events
		WHERE phone_number = $1
		ORDER BY requested_at DESC
		LIMIT 1
	`

	var latestRequestedAt time.Time

	err = tx.QueryRow(
		ctx,
		latestRequestQuery,
		phoneNumber,
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

	const countQuery = `
		SELECT COUNT(*)
		FROM otp_request_events
		WHERE phone_number = $1
		  AND requested_at >= $2
	`

	var requestCount int

	if err := tx.QueryRow(
		ctx,
		countQuery,
		phoneNumber,
		windowStartedAt,
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
			phone_number,
			requested_at
		)
		VALUES ($1, $2)
	`

	if _, err := tx.Exec(
		ctx,
		insertQuery,
		phoneNumber,
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
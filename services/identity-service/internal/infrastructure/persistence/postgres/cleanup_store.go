package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type CleanupStore struct {
	pool *pgxpool.Pool
}

func NewCleanupStore(pool *pgxpool.Pool) *CleanupStore {
	if pool == nil {
		panic("PostgreSQL pool is required")
	}

	return &CleanupStore{
		pool: pool,
	}
}

type CleanupResult struct {
	OTPRequestEventsDeleted int64
	OTPChallengesDeleted    int64
	AuthSessionsDeleted     int64
}

func (s *CleanupStore) Cleanup(
	ctx context.Context,
	now time.Time,
	otpRequestEventRetention time.Duration,
	otpChallengeRetention time.Duration,
	authSessionRetention time.Duration,
) (CleanupResult, error) {
	if now.IsZero() {
		return CleanupResult{}, fmt.Errorf("cleanup time must not be zero")
	}

	if otpRequestEventRetention <= 0 {
		return CleanupResult{}, fmt.Errorf(
			"OTP request event retention must be positive",
		)
	}

	if otpChallengeRetention <= 0 {
		return CleanupResult{}, fmt.Errorf(
			"OTP challenge retention must be positive",
		)
	}

	if authSessionRetention <= 0 {
		return CleanupResult{}, fmt.Errorf(
			"auth session retention must be positive",
		)
	}

	now = now.UTC()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return CleanupResult{}, fmt.Errorf(
			"begin cleanup transaction: %w",
			err,
		)
	}
	defer tx.Rollback(ctx)

	result := CleanupResult{}

	commandTag, err := tx.Exec(
		ctx,
		`
			DELETE FROM otp_request_events
			WHERE requested_at < $1
		`,
		now.Add(-otpRequestEventRetention),
	)
	if err != nil {
		return CleanupResult{}, fmt.Errorf(
			"delete old OTP request events: %w",
			err,
		)
	}

	result.OTPRequestEventsDeleted = commandTag.RowsAffected()

	commandTag, err = tx.Exec(
		ctx,
		`
			DELETE FROM otp_challenges
			WHERE expires_at < $1
		`,
		now.Add(-otpChallengeRetention),
	)
	if err != nil {
		return CleanupResult{}, fmt.Errorf(
			"delete old OTP challenges: %w",
			err,
		)
	}

	result.OTPChallengesDeleted = commandTag.RowsAffected()

	commandTag, err = tx.Exec(
		ctx,
		`
			DELETE FROM auth_sessions
			WHERE (
				revoked_at IS NOT NULL
				AND revoked_at < $1
			)
			OR expires_at < $1
		`,
		now.Add(-authSessionRetention),
	)
	if err != nil {
		return CleanupResult{}, fmt.Errorf(
			"delete old authentication sessions: %w",
			err,
		)
	}

	result.AuthSessionsDeleted = commandTag.RowsAffected()

	if err := tx.Commit(ctx); err != nil {
		return CleanupResult{}, fmt.Errorf(
			"commit cleanup transaction: %w",
			err,
		)
	}

	return result, nil
}

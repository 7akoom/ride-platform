package cleanup

import (
	"context"
	"log/slog"
	"time"

	postgresrepo "github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/persistence/postgres"
)

type Runner struct {
	store                    *postgresrepo.CleanupStore
	logger                   *slog.Logger
	interval                 time.Duration
	otpRequestEventRetention time.Duration
	otpChallengeRetention    time.Duration
	authSessionRetention     time.Duration
}

func NewRunner(
	store *postgresrepo.CleanupStore,
	logger *slog.Logger,
	interval time.Duration,
	otpRequestEventRetention time.Duration,
	otpChallengeRetention time.Duration,
	authSessionRetention time.Duration,
) *Runner {
	if store == nil {
		panic("cleanup store is required")
	}

	if logger == nil {
		panic("cleanup logger is required")
	}

	if interval <= 0 {
		panic("cleanup interval must be positive")
	}

	if otpRequestEventRetention <= 0 {
		panic("OTP request event retention must be positive")
	}

	if otpChallengeRetention <= 0 {
		panic("OTP challenge retention must be positive")
	}

	if authSessionRetention <= 0 {
		panic("auth session retention must be positive")
	}
	return &Runner{
		store:                    store,
		logger:                   logger,
		interval:                 interval,
		otpRequestEventRetention: otpRequestEventRetention,
		otpChallengeRetention:    otpChallengeRetention,
		authSessionRetention:     authSessionRetention,
	}
}

func (r *Runner) Run(ctx context.Context) {
	r.runOnce(ctx)

	ticker := time.NewTicker(
		r.interval,
	)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			r.logger.Info(
				"identity cleanup runner stopped",
			)
			return

		case <-ticker.C:
			r.runOnce(ctx)
		}
	}
}

func (r *Runner) runOnce(ctx context.Context) {
	result, err := r.store.Cleanup(
		ctx,
		time.Now().UTC(),
		r.otpRequestEventRetention,
		r.otpChallengeRetention,
		r.authSessionRetention,
	)
	if err != nil {
		if ctx.Err() != nil {
			return
		}

		r.logger.Error(
			"identity cleanup failed",
			"error", err,
		)
		return
	}

	r.logger.Info(
		"identity cleanup completed",
		"otp_request_events_deleted",
		result.OTPRequestEventsDeleted,
		"otp_challenges_deleted",
		result.OTPChallengesDeleted,
		"auth_sessions_deleted",
		result.AuthSessionsDeleted,
	)
}

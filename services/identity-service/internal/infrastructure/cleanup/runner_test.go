package cleanup

import (
	"io"
	"log/slog"
	"testing"
	"time"

	postgresrepo "github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/persistence/postgres"
)

func TestNewRunnerPanicsForInvalidConfiguration(
	t *testing.T,
) {
	validStore := &postgresrepo.CleanupStore{}

	validLogger := slog.New(
		slog.NewTextHandler(
			io.Discard,
			nil,
		),
	)

	const (
		validInterval                 = time.Minute
		validOTPRequestEventRetention = 24 * time.Hour
		validOTPChallengeRetention    = 24 * time.Hour
		validAuthSessionRetention     = 24 * time.Hour
	)

	tests := []struct {
		name                     string
		store                    *postgresrepo.CleanupStore
		logger                   *slog.Logger
		interval                 time.Duration
		otpRequestEventRetention time.Duration
		otpChallengeRetention    time.Duration
		authSessionRetention     time.Duration
	}{
		{
			name:                     "nil store",
			store:                    nil,
			logger:                   validLogger,
			interval:                 validInterval,
			otpRequestEventRetention: validOTPRequestEventRetention,
			otpChallengeRetention:    validOTPChallengeRetention,
			authSessionRetention:     validAuthSessionRetention,
		},
		{
			name:                     "nil logger",
			store:                    validStore,
			logger:                   nil,
			interval:                 validInterval,
			otpRequestEventRetention: validOTPRequestEventRetention,
			otpChallengeRetention:    validOTPChallengeRetention,
			authSessionRetention:     validAuthSessionRetention,
		},
		{
			name:                     "zero interval",
			store:                    validStore,
			logger:                   validLogger,
			interval:                 0,
			otpRequestEventRetention: validOTPRequestEventRetention,
			otpChallengeRetention:    validOTPChallengeRetention,
			authSessionRetention:     validAuthSessionRetention,
		},
		{
			name:                     "zero OTP request event retention",
			store:                    validStore,
			logger:                   validLogger,
			interval:                 validInterval,
			otpRequestEventRetention: 0,
			otpChallengeRetention:    validOTPChallengeRetention,
			authSessionRetention:     validAuthSessionRetention,
		},
		{
			name:                     "zero OTP challenge retention",
			store:                    validStore,
			logger:                   validLogger,
			interval:                 validInterval,
			otpRequestEventRetention: validOTPRequestEventRetention,
			otpChallengeRetention:    0,
			authSessionRetention:     validAuthSessionRetention,
		},
		{
			name:                     "zero auth session retention",
			store:                    validStore,
			logger:                   validLogger,
			interval:                 validInterval,
			otpRequestEventRetention: validOTPRequestEventRetention,
			otpChallengeRetention:    validOTPChallengeRetention,
			authSessionRetention:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if recovered := recover(); recovered == nil {
					t.Fatal(
						"NewRunner() did not panic for invalid configuration",
					)
				}
			}()

			NewRunner(
				tt.store,
				tt.logger,
				tt.interval,
				tt.otpRequestEventRetention,
				tt.otpChallengeRetention,
				tt.authSessionRetention,
			)
		})
	}
}

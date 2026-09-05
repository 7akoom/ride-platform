package auth

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func (s *service) Logout(
	ctx context.Context,
	input LogoutInput,
) error {
	startedAt := time.Now()

	recordAuthOutcome := func(
		outcome MetricOutcome,
	) {
		if s.metricsRecorder == nil {
			return
		}

		s.metricsRecorder.RecordAuthOperation(
			ctx,
			AuthMetricOperationLogout,
			outcome,
			time.Since(startedAt),
		)
	}

	recordSessionOutcome := func(
		outcome MetricOutcome,
	) {
		if s.metricsRecorder == nil {
			return
		}

		s.metricsRecorder.RecordSessionOperation(
			ctx,
			SessionMetricOperationRevoke,
			outcome,
		)
	}

	if strings.TrimSpace(input.RefreshToken) == "" {
		recordAuthOutcome(
			MetricOutcomeRejected,
		)

		return ErrInvalidRefreshToken
	}

	refreshTokenHash := s.refreshTokenHasher.Hash(
		input.RefreshToken,
	)

	if err := s.sessionRevocationStore.RevokeByRefreshTokenHash(
		ctx,
		refreshTokenHash,
		s.clock.Now().UTC(),
	); err != nil {
		recordSessionOutcome(
			MetricOutcomeFailed,
		)

		recordAuthOutcome(
			MetricOutcomeFailed,
		)

		return fmt.Errorf(
			"revoke authentication session: %w",
			err,
		)
	}

	recordSessionOutcome(
		MetricOutcomeSuccess,
	)

	recordAuthOutcome(
		MetricOutcomeSuccess,
	)

	return nil
}

func (s *service) LogoutAllSessions(
	ctx context.Context,
	input LogoutAllSessionsInput,
) error {
	startedAt := time.Now()

	recordAuthOutcome := func(
		outcome MetricOutcome,
	) {
		if s.metricsRecorder == nil {
			return
		}

		s.metricsRecorder.RecordAuthOperation(
			ctx,
			AuthMetricOperationLogout,
			outcome,
			time.Since(startedAt),
		)
	}

	recordSessionOutcome := func(
		outcome MetricOutcome,
	) {
		if s.metricsRecorder == nil {
			return
		}

		s.metricsRecorder.RecordSessionOperation(
			ctx,
			SessionMetricOperationRevokeAll,
			outcome,
		)
	}

	if strings.TrimSpace(input.RefreshToken) == "" {
		recordAuthOutcome(
			MetricOutcomeRejected,
		)

		return ErrInvalidRefreshToken
	}

	refreshTokenHash := s.refreshTokenHasher.Hash(
		input.RefreshToken,
	)

	if err := s.allSessionsRevocationStore.RevokeAllByRefreshTokenHash(
		ctx,
		refreshTokenHash,
		s.clock.Now().UTC(),
	); err != nil {
		recordSessionOutcome(
			MetricOutcomeFailed,
		)

		recordAuthOutcome(
			MetricOutcomeFailed,
		)

		return fmt.Errorf(
			"revoke all authentication sessions: %w",
			err,
		)
	}

	recordSessionOutcome(
		MetricOutcomeSuccess,
	)

	recordAuthOutcome(
		MetricOutcomeSuccess,
	)

	return nil
}

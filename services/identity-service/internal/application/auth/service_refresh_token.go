package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

func (s *service) RefreshToken(
	ctx context.Context,
	input RefreshTokenInput,
) (RefreshTokenResult, error) {
	startedAt := time.Now()

	recordAuthOutcome := func(
		outcome MetricOutcome,
	) {
		if s.metricsRecorder == nil {
			return
		}

		s.metricsRecorder.RecordAuthOperation(
			ctx,
			AuthMetricOperationRefresh,
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
			SessionMetricOperationRefresh,
			outcome,
		)
	}

	recordSecurityEvent := func(
		event SecurityMetricEvent,
	) {
		if s.metricsRecorder == nil {
			return
		}

		s.metricsRecorder.RecordSecurityEvent(
			ctx,
			event,
		)
	}

	if strings.TrimSpace(input.RefreshToken) == "" {
		recordAuthOutcome(
			MetricOutcomeRejected,
		)

		return RefreshTokenResult{},
			ErrInvalidRefreshToken
	}

	currentTokenHash := s.refreshTokenHasher.Hash(
		input.RefreshToken,
	)

	now := s.clock.Now().UTC()

	refreshContext, err :=
		s.refreshTokenRotationStore.Inspect(
			ctx,
			currentTokenHash,
			now,
		)
	if err != nil {
		mappedErr, securityEvent, handled :=
			classifyRefreshTokenDomainError(err)

		if handled {
			recordAuthOutcome(
				MetricOutcomeRejected,
			)

			if securityEvent != "" {
				recordSecurityEvent(
					securityEvent,
				)
			}

			return RefreshTokenResult{},
				mappedErr
		}

		recordAuthOutcome(
			MetricOutcomeFailed,
		)

		return RefreshTokenResult{}, fmt.Errorf(
			"inspect refresh token: %w",
			err,
		)
	}

	replacementRefreshToken, err :=
		s.refreshTokenGenerator.Generate()
	if err != nil {
		recordAuthOutcome(
			MetricOutcomeFailed,
		)

		return RefreshTokenResult{}, fmt.Errorf(
			"generate replacement refresh token: %w",
			err,
		)
	}

	replacementTokenHash := s.refreshTokenHasher.Hash(
		replacementRefreshToken,
	)

	replacementExpiresAt := now.Add(
		s.refreshTokenTTL,
	)

	if replacementExpiresAt.After(
		refreshContext.SessionExpiresAt,
	) {
		replacementExpiresAt =
			refreshContext.SessionExpiresAt
	}

	if !replacementExpiresAt.After(now) {
		recordAuthOutcome(
			MetricOutcomeRejected,
		)

		return RefreshTokenResult{},
			ErrSessionExpired
	}

	accessToken, accessTokenExpiresInSeconds, err :=
		s.accessTokenSigner.IssueForSession(
			refreshContext.IdentityID,
			refreshContext.SessionID,
			refreshContext.TenantHint,
			now,
			refreshContext.SessionExpiresAt,
		)
	if err != nil {
		recordAuthOutcome(
			MetricOutcomeFailed,
		)

		return RefreshTokenResult{}, fmt.Errorf(
			"issue refreshed access token: %w",
			err,
		)
	}

	err = s.refreshTokenRotationStore.Rotate(
		ctx,
		RefreshTokenRotationInput{
			CurrentTokenHash:     currentTokenHash,
			ReplacementTokenHash: replacementTokenHash,
			RotatedAt:            now,
			ReplacementExpiresAt: replacementExpiresAt,
		},
	)
	if err != nil {
		mappedErr, securityEvent, handled :=
			classifyRefreshTokenDomainError(err)

		if handled {
			recordAuthOutcome(
				MetricOutcomeRejected,
			)

			if securityEvent != "" {
				recordSecurityEvent(
					securityEvent,
				)
			}

			return RefreshTokenResult{},
				mappedErr
		}

		recordSessionOutcome(
			MetricOutcomeFailed,
		)

		recordAuthOutcome(
			MetricOutcomeFailed,
		)

		return RefreshTokenResult{}, fmt.Errorf(
			"rotate refresh token: %w",
			err,
		)
	}

	recordSessionOutcome(
		MetricOutcomeSuccess,
	)

	recordAuthOutcome(
		MetricOutcomeSuccess,
	)

	return RefreshTokenResult{
		IdentityID:                  refreshContext.IdentityID,
		AccessToken:                 accessToken,
		RefreshToken:                replacementRefreshToken,
		AccessTokenExpiresInSeconds: accessTokenExpiresInSeconds,
	}, nil
}

func classifyRefreshTokenDomainError(
	err error,
) (
	error,
	SecurityMetricEvent,
	bool,
) {
	switch {
	case errors.Is(
		err,
		ErrInvalidRefreshToken,
	):
		return ErrInvalidRefreshToken,
			"",
			true

	case errors.Is(
		err,
		ErrRefreshTokenExpired,
	):
		return ErrRefreshTokenExpired,
			"",
			true

	case errors.Is(
		err,
		ErrRefreshTokenRevoked,
	):
		return ErrRefreshTokenRevoked,
			"",
			true

	case errors.Is(
		err,
		ErrRefreshTokenReused,
	):
		return ErrRefreshTokenReused,
			SecurityMetricEventRefreshTokenReuse,
			true

	case errors.Is(
		err,
		ErrSessionExpired,
	):
		return ErrSessionExpired,
			"",
			true

	case errors.Is(
		err,
		ErrSessionRevoked,
	):
		return ErrSessionRevoked,
			"",
			true

	case errors.Is(
		err,
		ErrIdentityInactive,
	):
		return ErrIdentityInactive,
			"",
			true

	default:
		return nil,
			"",
			false
	}
}

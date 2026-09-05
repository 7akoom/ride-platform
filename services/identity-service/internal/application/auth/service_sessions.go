package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

func (s *service) ListMySessions(
	ctx context.Context,
	input ListMySessionsInput,
) (ListMySessionsResult, error) {
	identityID := strings.TrimSpace(input.IdentityID)
	currentSessionID := strings.TrimSpace(
		input.CurrentSessionID,
	)

	if identityID == "" {
		return ListMySessionsResult{},
			ErrIdentityNotFound
	}

	if currentSessionID == "" {
		return ListMySessionsResult{},
			ErrSessionNotFound
	}

	sessions, err := s.sessionReader.ListActiveByIdentity(
		ctx,
		identityID,
		s.clock.Now(),
	)
	if err != nil {
		return ListMySessionsResult{}, fmt.Errorf(
			"list authentication sessions: %w",
			err,
		)
	}

	result := ListMySessionsResult{
		Sessions: make(
			[]SessionInfo,
			0,
			len(sessions),
		),
	}

	for _, session := range sessions {
		result.Sessions = append(
			result.Sessions,
			SessionInfo{
				SessionID:  session.ID,
				ClientID:   session.ClientID,
				DeviceID:   session.DeviceID,
				DeviceName: session.DeviceName,
				Platform:   session.Platform,
				AppVersion: session.AppVersion,
				IPAddress:  session.IPAddress,
				UserAgent:  session.UserAgent,

				ExpiresAt:  session.ExpiresAt,
				LastSeenAt: session.LastSeenAt,
				CreatedAt:  session.CreatedAt,

				IsCurrent: session.ID == currentSessionID,
			},
		)
	}

	return result, nil
}

func (s *service) RevokeSession(
	ctx context.Context,
	input RevokeSessionInput,
) error {
	identityID := strings.TrimSpace(input.IdentityID)
	sessionID := strings.TrimSpace(input.SessionID)

	if identityID == "" {
		return ErrIdentityNotFound
	}

	if sessionID == "" {
		return ErrSessionNotFound
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

	err := s.sessionManagementRevocationStore.RevokeSession(
		ctx,
		identityID,
		sessionID,
		s.clock.Now(),
	)
	if err != nil {
		if errors.Is(
			err,
			ErrSessionNotFound,
		) {
			recordSessionOutcome(
				MetricOutcomeRejected,
			)

			return ErrSessionNotFound
		}

		recordSessionOutcome(
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

	return nil
}

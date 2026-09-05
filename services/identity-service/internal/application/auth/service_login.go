package auth

import (
	"context"
	"errors"
	"fmt"
	"time"
)

func (s *service) completeIdentifierLogin(
	ctx context.Context,
	challenge OTPChallenge,
	verifiedAt time.Time,
	metadata SessionMetadata,
) (VerifyOTPResult, error) {
	if s.identityIdentifierRepository == nil {
		return VerifyOTPResult{}, errors.New(
			"identity identifier repository is not configured",
		)
	}

	if challenge.TargetIdentityID != nil {
		return VerifyOTPResult{}, errors.New(
			"login OTP challenge cannot target an identity",
		)
	}

	identifier, err := NewIdentifier(
		challenge.Identifier.Type,
		challenge.Identifier.Value,
	)
	if err != nil {
		return VerifyOTPResult{}, fmt.Errorf(
			"validate login OTP identifier: %w",
			err,
		)
	}

	identity, found, err :=
		s.identityIdentifierRepository.FindIdentityByIdentifier(
			ctx,
			identifier,
		)
	if err != nil {
		return VerifyOTPResult{}, fmt.Errorf(
			"find identity by identifier: %w",
			err,
		)
	}

	if !found {
		identity, err =
			s.identityIdentifierRepository.
				CreateIdentityWithIdentifier(
					ctx,
					identifier,
					verifiedAt,
				)
		if err != nil {
			return VerifyOTPResult{}, fmt.Errorf(
				"create identity with identifier: %w",
				err,
			)
		}
	}

	return s.issueLoginTokensWithSessionMetadata(
		ctx,
		challenge,
		identity,
		verifiedAt,
		metadata,
	)
}

func (s *service) issueLoginTokensWithSessionMetadata(
	ctx context.Context,
	challenge OTPChallenge,
	identity Identity,
	verifiedAt time.Time,
	metadata SessionMetadata,
) (VerifyOTPResult, error) {
	if !identity.IsActive {
		return VerifyOTPResult{},
			ErrIdentityInactive
	}

	recordSessionOutcome := func(
		outcome MetricOutcome,
	) {
		if s.metricsRecorder == nil {
			return
		}

		s.metricsRecorder.RecordSessionOperation(
			ctx,
			SessionMetricOperationCreate,
			outcome,
		)
	}

	tokenPair, err := s.tokenIssuer.Issue(
		ctx,
		TokenIssueInput{
			Identity:        identity,
			ChallengeID:     challenge.ID,
			VerifiedAt:      verifiedAt,
			TenantHint:      challenge.TenantHint,
			SessionMetadata: metadata,
		},
	)
	if err != nil {
		switch {
		case errors.Is(
			err,
			ErrChallengeNotFound,
		):
			recordSessionOutcome(
				MetricOutcomeRejected,
			)

			return VerifyOTPResult{},
				ErrChallengeNotFound

		case errors.Is(
			err,
			ErrChallengeExpired,
		):
			recordSessionOutcome(
				MetricOutcomeRejected,
			)

			return VerifyOTPResult{},
				ErrChallengeExpired

		case errors.Is(
			err,
			ErrChallengeUsed,
		):
			recordSessionOutcome(
				MetricOutcomeRejected,
			)

			return VerifyOTPResult{},
				ErrChallengeUsed

		case errors.Is(
			err,
			ErrChallengeCancelled,
		):
			recordSessionOutcome(
				MetricOutcomeRejected,
			)

			return VerifyOTPResult{},
				ErrChallengeCancelled

		case errors.Is(
			err,
			ErrChallengeAttemptsExceeded,
		):
			recordSessionOutcome(
				MetricOutcomeRejected,
			)

			return VerifyOTPResult{},
				ErrChallengeAttemptsExceeded

		default:
			recordSessionOutcome(
				MetricOutcomeFailed,
			)

			return VerifyOTPResult{}, fmt.Errorf(
				"issue token pair: %w",
				err,
			)
		}
	}

	recordSessionOutcome(
		MetricOutcomeSuccess,
	)

	return VerifyOTPResult{
		IdentityID:                  identity.ID,
		AccessToken:                 tokenPair.AccessToken,
		RefreshToken:                tokenPair.RefreshToken,
		AccessTokenExpiresInSeconds: tokenPair.AccessTokenExpiresInSeconds,
	}, nil
}

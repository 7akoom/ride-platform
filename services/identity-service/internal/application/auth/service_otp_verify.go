package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

func (s *service) VerifyOTP(
	ctx context.Context,
	input VerifyOTPInput,
) (VerifyOTPResult, error) {
	verificationStartedAt := time.Now()
	expectedPurpose, err := ParseOTPPurpose(
		string(input.ExpectedPurpose),
	)
	if err != nil {
		return VerifyOTPResult{}, err
	}

	recordVerificationOutcome := func(
		outcome MetricOutcome,
	) {
		if s.metricsRecorder == nil {
			return
		}

		s.metricsRecorder.RecordOTPVerification(
			ctx,
			expectedPurpose,
			outcome,
		)

		if expectedPurpose ==
			OTPPurposeLogin {
			s.metricsRecorder.RecordAuthOperation(
				ctx,
				AuthMetricOperationLogin,
				outcome,
				time.Since(
					verificationStartedAt,
				),
			)
		}
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

	challenge, err := s.challengeRepository.FindByID(
		ctx,
		input.ChallengeID,
	)
	if err != nil {
		if errors.Is(
			err,
			ErrChallengeNotFound,
		) {
			recordVerificationOutcome(
				MetricOutcomeRejected,
			)

			return VerifyOTPResult{},
				ErrChallengeNotFound
		}

		recordVerificationOutcome(
			MetricOutcomeFailed,
		)

		return VerifyOTPResult{}, fmt.Errorf(
			"find OTP challenge: %w",
			err,
		)
	}

	if challenge.Purpose != expectedPurpose {
		recordVerificationOutcome(
			MetricOutcomeRejected,
		)

		return VerifyOTPResult{},
			ErrOTPPurposeMismatch
	}

	if expectedPurpose == OTPPurposeLinkIdentifier ||
		expectedPurpose == OTPPurposeUnlinkIdentifier {
		if input.ExpectedTargetIdentityID == nil ||
			challenge.TargetIdentityID == nil {
			recordVerificationOutcome(
				MetricOutcomeRejected,
			)

			return VerifyOTPResult{},
				ErrOTPChallengeTargetMismatch
		}

		expectedTargetIdentityID := strings.TrimSpace(
			*input.ExpectedTargetIdentityID,
		)

		challengeTargetIdentityID := strings.TrimSpace(
			*challenge.TargetIdentityID,
		)

		if expectedTargetIdentityID == "" ||
			challengeTargetIdentityID == "" ||
			expectedTargetIdentityID != challengeTargetIdentityID {
			recordVerificationOutcome(
				MetricOutcomeRejected,
			)

			return VerifyOTPResult{},
				ErrOTPChallengeTargetMismatch
		}
	}

	if challenge.VerifiedAt != nil {
		recordVerificationOutcome(
			MetricOutcomeRejected,
		)

		recordSecurityEvent(
			SecurityMetricEventChallengeReplay,
		)

		return VerifyOTPResult{},
			ErrChallengeUsed
	}

	if challenge.CancelledAt != nil {
		recordVerificationOutcome(
			MetricOutcomeRejected,
		)

		return VerifyOTPResult{},
			ErrChallengeCancelled
	}

	now := s.clock.Now()

	if !now.Before(challenge.ExpiresAt) {
		recordVerificationOutcome(
			MetricOutcomeRejected,
		)

		return VerifyOTPResult{},
			ErrChallengeExpired
	}

	if challenge.FailedAttempts >=
		challenge.MaxAttempts {
		recordVerificationOutcome(
			MetricOutcomeRejected,
		)

		recordSecurityEvent(
			SecurityMetricEventOTPAttemptsExceeded,
		)

		return VerifyOTPResult{},
			ErrChallengeAttemptsExceeded
	}

	otpMatches, err := s.otpHasher.Compare(
		challenge.CodeHash,
		challenge.ID,
		input.Code,
	)
	if err != nil {
		recordVerificationOutcome(
			MetricOutcomeFailed,
		)

		return VerifyOTPResult{}, fmt.Errorf(
			"compare OTP: %w",
			err,
		)
	}

	if !otpMatches {
		recordErr :=
			s.challengeRepository.RecordFailedAttempt(
				ctx,
				challenge.ID,
				now,
			)

		if recordErr != nil {
			switch {
			case errors.Is(
				recordErr,
				ErrChallengeNotFound,
			):
				recordVerificationOutcome(
					MetricOutcomeRejected,
				)

				return VerifyOTPResult{},
					ErrChallengeNotFound

			case errors.Is(
				recordErr,
				ErrChallengeExpired,
			):
				recordVerificationOutcome(
					MetricOutcomeRejected,
				)

				return VerifyOTPResult{},
					ErrChallengeExpired

			case errors.Is(
				recordErr,
				ErrChallengeUsed,
			):
				recordVerificationOutcome(
					MetricOutcomeRejected,
				)

				recordSecurityEvent(
					SecurityMetricEventChallengeReplay,
				)

				return VerifyOTPResult{},
					ErrChallengeUsed

			case errors.Is(
				recordErr,
				ErrChallengeCancelled,
			):
				recordVerificationOutcome(
					MetricOutcomeRejected,
				)

				return VerifyOTPResult{},
					ErrChallengeCancelled

			case errors.Is(
				recordErr,
				ErrChallengeAttemptsExceeded,
			):
				recordVerificationOutcome(
					MetricOutcomeRejected,
				)

				recordSecurityEvent(
					SecurityMetricEventOTPAttemptsExceeded,
				)

				return VerifyOTPResult{},
					ErrChallengeAttemptsExceeded

			default:
				recordVerificationOutcome(
					MetricOutcomeFailed,
				)

				return VerifyOTPResult{}, fmt.Errorf(
					"record failed OTP attempt: %w",
					recordErr,
				)
			}
		}

		recordVerificationOutcome(
			MetricOutcomeRejected,
		)

		return VerifyOTPResult{},
			ErrInvalidOTP
	}

	var result VerifyOTPResult

	switch challenge.Purpose {
	case OTPPurposeLogin:
		result, err = s.completeIdentifierLogin(
			ctx,
			challenge,
			now,
			input.SessionMetadata,
		)

	case OTPPurposeLinkIdentifier:
		result, err = s.completeIdentifierLink(
			ctx,
			challenge,
			now,
		)

	case OTPPurposeUnlinkIdentifier:
		result, err = s.completeIdentifierUnlink(
			ctx,
			challenge,
			now,
		)

	default:
		recordVerificationOutcome(
			MetricOutcomeFailed,
		)

		return VerifyOTPResult{},
			ErrInvalidOTPPurpose
	}

	if err != nil {
		switch {
		case errors.Is(
			err,
			ErrChallengeUsed,
		):
			recordVerificationOutcome(
				MetricOutcomeRejected,
			)

			recordSecurityEvent(
				SecurityMetricEventChallengeReplay,
			)

		case errors.Is(
			err,
			ErrChallengeAttemptsExceeded,
		):
			recordVerificationOutcome(
				MetricOutcomeRejected,
			)

			recordSecurityEvent(
				SecurityMetricEventOTPAttemptsExceeded,
			)

		case errors.Is(
			err,
			ErrChallengeNotFound,
		),
			errors.Is(
				err,
				ErrChallengeExpired,
			),
			errors.Is(
				err,
				ErrChallengeCancelled,
			),
			errors.Is(
				err,
				ErrIdentityInactive,
			),
			errors.Is(
				err,
				ErrIdentifierAlreadyLinked,
			),
			errors.Is(
				err,
				ErrIdentityNotFound,
			),
			errors.Is(
				err,
				ErrIdentifierNotLinked,
			),
			errors.Is(
				err,
				ErrLastIdentifierRemoval,
			):
			recordVerificationOutcome(
				MetricOutcomeRejected,
			)

		default:
			recordVerificationOutcome(
				MetricOutcomeFailed,
			)
		}

		return VerifyOTPResult{}, err
	}

	recordVerificationOutcome(
		MetricOutcomeSuccess,
	)

	return result, nil
}

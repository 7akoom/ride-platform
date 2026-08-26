package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

func (s *service) VerifyOTP(
	ctx context.Context,
	input VerifyOTPInput,
) (VerifyOTPResult, error) {
	expectedPurpose, err := ParseOTPPurpose(
		string(input.ExpectedPurpose),
	)
	if err != nil {
		return VerifyOTPResult{}, err
	}
	challenge, err := s.challengeRepository.FindByID(
		ctx,
		input.ChallengeID,
	)
	if err != nil {
		if errors.Is(err, ErrChallengeNotFound) {
			return VerifyOTPResult{}, ErrChallengeNotFound
		}

		return VerifyOTPResult{}, fmt.Errorf(
			"find OTP challenge: %w",
			err,
		)
	}

	if challenge.Purpose != expectedPurpose {
		return VerifyOTPResult{}, ErrOTPPurposeMismatch
	}

	if expectedPurpose == OTPPurposeLinkIdentifier ||
		expectedPurpose == OTPPurposeUnlinkIdentifier {
		if input.ExpectedTargetIdentityID == nil ||
			challenge.TargetIdentityID == nil {
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
			return VerifyOTPResult{},
				ErrOTPChallengeTargetMismatch
		}
	}

	if challenge.VerifiedAt != nil {
		return VerifyOTPResult{}, ErrChallengeUsed
	}

	if challenge.CancelledAt != nil {
		return VerifyOTPResult{}, ErrChallengeCancelled
	}

	now := s.clock.Now()

	if !now.Before(challenge.ExpiresAt) {
		return VerifyOTPResult{}, ErrChallengeExpired
	}

	if challenge.FailedAttempts >= challenge.MaxAttempts {
		return VerifyOTPResult{},
			ErrChallengeAttemptsExceeded
	}

	otpMatches, err := s.otpHasher.Compare(
		challenge.CodeHash,
		challenge.ID,
		input.Code,
	)
	if err != nil {
		return VerifyOTPResult{}, fmt.Errorf(
			"compare OTP: %w",
			err,
		)
	}

	if !otpMatches {
		recordErr := s.challengeRepository.RecordFailedAttempt(
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
				return VerifyOTPResult{},
					ErrChallengeNotFound

			case errors.Is(
				recordErr,
				ErrChallengeExpired,
			):
				return VerifyOTPResult{},
					ErrChallengeExpired

			case errors.Is(
				recordErr,
				ErrChallengeUsed,
			):
				return VerifyOTPResult{},
					ErrChallengeUsed

			case errors.Is(
				recordErr,
				ErrChallengeCancelled,
			):
				return VerifyOTPResult{},
					ErrChallengeCancelled

			case errors.Is(
				recordErr,
				ErrChallengeAttemptsExceeded,
			):
				return VerifyOTPResult{},
					ErrChallengeAttemptsExceeded

			default:
				return VerifyOTPResult{}, fmt.Errorf(
					"record failed OTP attempt: %w",
					recordErr,
				)
			}
		}

		return VerifyOTPResult{}, ErrInvalidOTP
	}

	switch challenge.Purpose {
	case OTPPurposeLogin:
		return s.completeIdentifierLogin(
			ctx,
			challenge,
			now,
			input.SessionMetadata,
		)

	case OTPPurposeLinkIdentifier:
		return s.completeIdentifierLink(
			ctx,
			challenge,
			now,
		)

	case OTPPurposeUnlinkIdentifier:
		return s.completeIdentifierUnlink(
			ctx,
			challenge,
			now,
		)

	default:
		return VerifyOTPResult{},
			ErrInvalidOTPPurpose
	}
}

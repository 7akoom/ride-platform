package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

func (s *service) completeIdentifierLink(
	ctx context.Context,
	challenge OTPChallenge,
	verifiedAt time.Time,
) (VerifyOTPResult, error) {
	if s.identifierLinkCompletionStore == nil {
		return VerifyOTPResult{}, errors.New(
			"identifier link completion store is not configured",
		)
	}

	if challenge.TargetIdentityID == nil {
		return VerifyOTPResult{}, errors.New(
			"link identifier OTP challenge has no target identity",
		)
	}

	identityID := strings.TrimSpace(
		*challenge.TargetIdentityID,
	)
	if identityID == "" {
		return VerifyOTPResult{}, errors.New(
			"link identifier OTP challenge target identity is blank",
		)
	}

	identifier, err := NewIdentifier(
		challenge.Identifier.Type,
		challenge.Identifier.Value,
	)
	if err != nil {
		return VerifyOTPResult{}, fmt.Errorf(
			"validate link identifier OTP identifier: %w",
			err,
		)
	}

	err = s.identifierLinkCompletionStore.Complete(
		ctx,
		IdentifierLinkCompletionInput{
			ChallengeID: challenge.ID,
			IdentityID:  identityID,
			Identifier:  identifier,
			VerifiedAt:  verifiedAt,
		},
	)
	if err != nil {
		switch {
		case errors.Is(
			err,
			ErrChallengeNotFound,
		):
			return VerifyOTPResult{},
				ErrChallengeNotFound

		case errors.Is(
			err,
			ErrChallengeExpired,
		):
			return VerifyOTPResult{},
				ErrChallengeExpired

		case errors.Is(
			err,
			ErrChallengeUsed,
		):
			return VerifyOTPResult{},
				ErrChallengeUsed

		case errors.Is(
			err,
			ErrChallengeCancelled,
		):
			return VerifyOTPResult{},
				ErrChallengeCancelled

		case errors.Is(
			err,
			ErrChallengeAttemptsExceeded,
		):
			return VerifyOTPResult{},
				ErrChallengeAttemptsExceeded

		case errors.Is(
			err,
			ErrIdentifierAlreadyLinked,
		):
			return VerifyOTPResult{},
				ErrIdentifierAlreadyLinked

		default:
			return VerifyOTPResult{}, fmt.Errorf(
				"complete identifier link: %w",
				err,
			)
		}
	}

	return VerifyOTPResult{
		IdentityID: identityID,
	}, nil
}

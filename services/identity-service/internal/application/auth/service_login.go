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

		default:
			return VerifyOTPResult{}, fmt.Errorf(
				"issue token pair: %w",
				err,
			)
		}
	}

	return VerifyOTPResult{
		IdentityID:                  identity.ID,
		AccessToken:                 tokenPair.AccessToken,
		RefreshToken:                tokenPair.RefreshToken,
		AccessTokenExpiresInSeconds: tokenPair.AccessTokenExpiresInSeconds,
	}, nil
}

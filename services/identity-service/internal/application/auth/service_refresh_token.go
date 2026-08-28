package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

func (s *service) RefreshToken(
	ctx context.Context,
	input RefreshTokenInput,
) (RefreshTokenResult, error) {
	if strings.TrimSpace(input.RefreshToken) == "" {
		return RefreshTokenResult{}, ErrInvalidRefreshToken
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
		switch {
		case errors.Is(err, ErrInvalidRefreshToken):
			return RefreshTokenResult{},
				ErrInvalidRefreshToken

		case errors.Is(err, ErrRefreshTokenExpired):
			return RefreshTokenResult{},
				ErrRefreshTokenExpired

		case errors.Is(err, ErrRefreshTokenRevoked):
			return RefreshTokenResult{},
				ErrRefreshTokenRevoked

		case errors.Is(err, ErrRefreshTokenReused):
			return RefreshTokenResult{},
				ErrRefreshTokenReused

		case errors.Is(err, ErrSessionExpired):
			return RefreshTokenResult{},
				ErrSessionExpired

		case errors.Is(err, ErrSessionRevoked):
			return RefreshTokenResult{},
				ErrSessionRevoked

		case errors.Is(err, ErrIdentityInactive):
			return RefreshTokenResult{},
				ErrIdentityInactive

		default:
			return RefreshTokenResult{}, fmt.Errorf(
				"inspect refresh token: %w",
				err,
			)
		}
	}

	replacementRefreshToken, err :=
		s.refreshTokenGenerator.Generate()
	if err != nil {
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
		switch {
		case errors.Is(err, ErrInvalidRefreshToken):
			return RefreshTokenResult{},
				ErrInvalidRefreshToken

		case errors.Is(err, ErrRefreshTokenExpired):
			return RefreshTokenResult{},
				ErrRefreshTokenExpired

		case errors.Is(err, ErrRefreshTokenRevoked):
			return RefreshTokenResult{},
				ErrRefreshTokenRevoked

		case errors.Is(err, ErrRefreshTokenReused):
			return RefreshTokenResult{},
				ErrRefreshTokenReused

		case errors.Is(err, ErrSessionExpired):
			return RefreshTokenResult{},
				ErrSessionExpired

		case errors.Is(err, ErrSessionRevoked):
			return RefreshTokenResult{},
				ErrSessionRevoked

		case errors.Is(err, ErrIdentityInactive):
			return RefreshTokenResult{},
				ErrIdentityInactive

		default:
			return RefreshTokenResult{}, fmt.Errorf(
				"rotate refresh token: %w",
				err,
			)
		}
	}

	return RefreshTokenResult{
		IdentityID:                  refreshContext.IdentityID,
		AccessToken:                 accessToken,
		RefreshToken:                replacementRefreshToken,
		AccessTokenExpiresInSeconds: accessTokenExpiresInSeconds,
	}, nil
}

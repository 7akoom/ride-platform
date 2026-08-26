package auth

import (
	"context"
	"fmt"
	"strings"
)

func (s *service) Logout(
	ctx context.Context,
	input LogoutInput,
) error {
	if strings.TrimSpace(input.RefreshToken) == "" {
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
		return fmt.Errorf(
			"revoke authentication session: %w",
			err,
		)
	}

	return nil
}

func (s *service) LogoutAllSessions(
	ctx context.Context,
	input LogoutAllSessionsInput,
) error {
	if strings.TrimSpace(input.RefreshToken) == "" {
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
		return fmt.Errorf(
			"revoke all authentication sessions: %w",
			err,
		)
	}

	return nil
}

package token

import "github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"

type RefreshTokenHasher struct{}

var _ auth.RefreshTokenHasher = (*RefreshTokenHasher)(nil)

func NewRefreshTokenHasher() *RefreshTokenHasher {
	return &RefreshTokenHasher{}
}

func (h *RefreshTokenHasher) Hash(
	refreshToken string,
) string {
	return HashRefreshToken(refreshToken)
}
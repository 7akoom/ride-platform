package token

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

const refreshTokenRandomBytes = 32

type RefreshTokenGenerator struct{}

func NewRefreshTokenGenerator() *RefreshTokenGenerator {
	return &RefreshTokenGenerator{}
}

func (g *RefreshTokenGenerator) Generate() (string, error) {
	randomBytes := make([]byte, refreshTokenRandomBytes)

	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf(
			"generate secure refresh token: %w",
			err,
		)
	}

	encoded := base64.RawURLEncoding.EncodeToString(randomBytes)

	return "rt_" + encoded, nil
}

func HashRefreshToken(refreshToken string) string {
	hash := sha256.Sum256([]byte(refreshToken))

	return hex.EncodeToString(hash[:])
}
package token

import (
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
)

func TestRefreshTokenGeneratorGenerate(t *testing.T) {
	generator := NewRefreshTokenGenerator()

	refreshToken, err := generator.Generate()
	if err != nil {
		t.Fatalf("Generate() returned an error: %v", err)
	}

	const prefix = "rt_"

	if !strings.HasPrefix(refreshToken, prefix) {
		t.Fatalf(
			"Generate() returned token without expected prefix: %q",
			refreshToken,
		)
	}

	encoded := strings.TrimPrefix(refreshToken, prefix)

	randomBytes, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf(
			"generated refresh token contains invalid base64url payload: %v",
			err,
		)
	}

	if len(randomBytes) != refreshTokenRandomBytes {
		t.Fatalf(
			"generated refresh token contains %d random bytes, expected %d",
			len(randomBytes),
			refreshTokenRandomBytes,
		)
	}
}

func TestHashRefreshTokenReturnsSHA256Hex(t *testing.T) {
	const refreshToken = "rt_example-refresh-token"

	hash := HashRefreshToken(refreshToken)

	if len(hash) != 64 {
		t.Fatalf(
			"HashRefreshToken() returned hash length %d, expected 64",
			len(hash),
		)
	}

	if _, err := hex.DecodeString(hash); err != nil {
		t.Fatalf(
			"HashRefreshToken() returned invalid hexadecimal hash: %v",
			err,
		)
	}

	if hash == refreshToken {
		t.Fatal(
			"HashRefreshToken() returned the raw refresh token",
		)
	}
}
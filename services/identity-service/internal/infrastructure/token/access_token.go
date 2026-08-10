package token

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const accessTokenIDRandomBytes = 16

type AccessTokenClaims struct {
	SessionID string `json:"sid"`

	jwt.RegisteredClaims
}

type AccessTokenSigner struct {
	privateKey ed25519.PrivateKey
	issuer     string
	audience   string
	keyID      string
	ttl        time.Duration
}

func NewAccessTokenSigner(
	privateKeyPath string,
	issuer string,
	audience string,
	keyID string,
	ttl time.Duration,
) (*AccessTokenSigner, error) {
	if privateKeyPath == "" {
		return nil, errors.New("access token private key path cannot be empty")
	}

	if issuer == "" {
		return nil, errors.New("access token issuer cannot be empty")
	}

	if audience == "" {
		return nil, errors.New("access token audience cannot be empty")
	}

	if keyID == "" {
		return nil, errors.New("access token key ID cannot be empty")
	}

	if ttl <= 0 {
		return nil, errors.New("access token TTL must be greater than zero")
	}

	privateKeyPEM, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return nil, fmt.Errorf(
			"read access token private key: %w",
			err,
		)
	}

	parsedPrivateKey, err := jwt.ParseEdPrivateKeyFromPEM(
		privateKeyPEM,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"parse access token private key: %w",
			err,
		)
	}

	privateKey, ok := parsedPrivateKey.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New(
			"access token private key is not an Ed25519 private key",
		)
	}

	return &AccessTokenSigner{
		privateKey: privateKey,
		issuer:     issuer,
		audience:   audience,
		keyID:      keyID,
		ttl:        ttl,
	}, nil
}

func (s *AccessTokenSigner) Issue(
	identityID string,
	sessionID string,
	issuedAt time.Time,
) (string, int32, error) {
	if identityID == "" {
		return "", 0, errors.New("identity ID cannot be empty")
	}

	if sessionID == "" {
		return "", 0, errors.New("session ID cannot be empty")
	}

	tokenID, err := generateAccessTokenID()
	if err != nil {
		return "", 0, err
	}

	issuedAt = issuedAt.UTC()
	expiresAt := issuedAt.Add(s.ttl)

	claims := AccessTokenClaims{
		SessionID: sessionID,

		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.issuer,
			Subject:   identityID,
			Audience:  jwt.ClaimStrings{s.audience},
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(issuedAt),
			ID:        tokenID,
		},
	}

	accessToken := jwt.NewWithClaims(
		jwt.SigningMethodEdDSA,
		claims,
	)

	accessToken.Header["kid"] = s.keyID

	signedToken, err := accessToken.SignedString(
		s.privateKey,
	)
	if err != nil {
		return "", 0, fmt.Errorf(
			"sign access token: %w",
			err,
		)
	}

	return signedToken, int32(s.ttl.Seconds()), nil
}

func generateAccessTokenID() (string, error) {
	randomBytes := make([]byte, accessTokenIDRandomBytes)

	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf(
			"generate access token ID: %w",
			err,
		)
	}

	encoded := base64.RawURLEncoding.EncodeToString(randomBytes)

	return "at_" + encoded, nil
}
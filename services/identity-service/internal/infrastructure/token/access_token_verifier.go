package token

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"os"

	"github.com/golang-jwt/jwt/v5"
)

var ErrAccessTokenRevoked = errors.New(
	"access token session has been revoked",
)

type SessionAccessRevocationChecker interface {
	IsRevoked(
		ctx context.Context,
		sessionID string,
	) (bool, error)
}

type AccessTokenVerifier struct {
	publicKey         ed25519.PublicKey
	issuer            string
	audience          string
	keyID             string
	revocationChecker SessionAccessRevocationChecker
}

func NewAccessTokenVerifier(
	publicKeyPath string,
	issuer string,
	audience string,
	keyID string,
	revocationChecker SessionAccessRevocationChecker,
) (*AccessTokenVerifier, error) {
	if publicKeyPath == "" {
		return nil, errors.New(
			"access token public key path cannot be empty",
		)
	}

	if issuer == "" {
		return nil, errors.New(
			"access token issuer cannot be empty",
		)
	}

	if audience == "" {
		return nil, errors.New(
			"access token audience cannot be empty",
		)
	}

	if keyID == "" {
		return nil, errors.New(
			"access token key ID cannot be empty",
		)
	}

	if revocationChecker == nil {
		return nil, errors.New(
			"session access revocation checker is required",
		)
	}

	publicKeyPEM, err := os.ReadFile(
		publicKeyPath,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"read access token public key: %w",
			err,
		)
	}

	parsedPublicKey, err := jwt.ParseEdPublicKeyFromPEM(
		publicKeyPEM,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"parse access token public key: %w",
			err,
		)
	}

	publicKey, ok := parsedPublicKey.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New(
			"access token public key is not an Ed25519 public key",
		)
	}

	return &AccessTokenVerifier{
		publicKey:         publicKey,
		issuer:            issuer,
		audience:          audience,
		keyID:             keyID,
		revocationChecker: revocationChecker,
	}, nil
}

func (v *AccessTokenVerifier) Verify(
	ctx context.Context,
	rawToken string,
) (AccessTokenClaims, error) {
	if rawToken == "" {
		return AccessTokenClaims{}, errors.New(
			"access token cannot be empty",
		)
	}

	claims := AccessTokenClaims{}

	parsedToken, err := jwt.ParseWithClaims(
		rawToken,
		&claims,
		func(parsedToken *jwt.Token) (any, error) {
			if parsedToken.Method.Alg() !=
				jwt.SigningMethodEdDSA.Alg() {
				return nil, fmt.Errorf(
					"unexpected access token signing method: %s",
					parsedToken.Method.Alg(),
				)
			}

			keyID, ok := parsedToken.Header["kid"].(string)
			if !ok || keyID == "" {
				return nil, errors.New(
					"access token key ID is missing",
				)
			}

			if keyID != v.keyID {
				return nil, errors.New(
					"access token key ID is invalid",
				)
			}

			return v.publicKey, nil
		},
		jwt.WithValidMethods(
			[]string{
				jwt.SigningMethodEdDSA.Alg(),
			},
		),
		jwt.WithStrictDecoding(),
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(v.audience),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
	)
	if err != nil {
		return AccessTokenClaims{}, fmt.Errorf(
			"verify access token: %w",
			err,
		)
	}

	if parsedToken == nil || !parsedToken.Valid {
		return AccessTokenClaims{}, errors.New(
			"access token is invalid",
		)
	}

	if claims.Subject == "" {
		return AccessTokenClaims{}, errors.New(
			"access token subject is missing",
		)
	}

	if claims.SessionID == "" {
		return AccessTokenClaims{}, errors.New(
			"access token session ID is missing",
		)
	}

	if claims.IssuedAt == nil {
		return AccessTokenClaims{}, errors.New(
			"access token issued-at claim is missing",
		)
	}

	if claims.ID == "" {
		return AccessTokenClaims{}, errors.New(
			"access token ID is missing",
		)
	}

	revoked, err := v.revocationChecker.IsRevoked(
		ctx,
		claims.SessionID,
	)
	if err != nil {
		return AccessTokenClaims{}, fmt.Errorf(
			"check access token session revocation: %w",
			err,
		)
	}

	if revoked {
		return AccessTokenClaims{},
			ErrAccessTokenRevoked
	}

	return claims, nil
}

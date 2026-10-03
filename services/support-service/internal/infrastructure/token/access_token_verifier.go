package token

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"os"

	"github.com/golang-jwt/jwt/v5"
)

// AccessTokenClaims mirrors the shape identity-service signs into every
// access token (see identity-service/internal/infrastructure/token).
// Downstream services only ever verify tokens, never issue them.
type AccessTokenClaims struct {
	SessionID  string `json:"sid"`
	TenantHint string `json:"tenant_hint,omitempty"`

	jwt.RegisteredClaims
}

// AccessTokenVerifier checks that a token was signed by identity-service's
// active key, for the right issuer/audience, and hasn't expired.
//
// Unlike identity-service's own verifier, this one does NOT check live
// session revocation — that would mean calling back into identity-service's
// session store on every request, which no RPC exposes today. This is a
// deliberate MVP trade-off: access tokens are short-lived (15m by
// default), so a revoked token stays usable against this service for at
// most that long. Revisit if that window becomes a real risk, e.g. by
// adding a lightweight revocation-check RPC to identity-service.
//
// It also only trusts a single active signing key (no keyring/rotation
// support like identity-service's verifier has) — simpler to operate for
// now; rotating identity's key means updating every downstream service's
// public key file and restarting it.
type AccessTokenVerifier struct {
	publicKey ed25519.PublicKey
	issuer    string
	audience  string
	keyID     string
}

func NewAccessTokenVerifier(
	publicKeyPath string,
	issuer string,
	audience string,
	keyID string,
) (*AccessTokenVerifier, error) {
	if publicKeyPath == "" {
		return nil, errors.New("access token public key path cannot be empty")
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

	publicKeyPEM, err := os.ReadFile(publicKeyPath)
	if err != nil {
		return nil, fmt.Errorf("read access token public key: %w", err)
	}

	parsedPublicKey, err := jwt.ParseEdPublicKeyFromPEM(publicKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("parse access token public key: %w", err)
	}

	publicKey, ok := parsedPublicKey.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("access token public key is not an Ed25519 public key")
	}

	return &AccessTokenVerifier{
		publicKey: publicKey,
		issuer:    issuer,
		audience:  audience,
		keyID:     keyID,
	}, nil
}

func (v *AccessTokenVerifier) Verify(rawToken string) (AccessTokenClaims, error) {
	if rawToken == "" {
		return AccessTokenClaims{}, errors.New("access token cannot be empty")
	}

	claims := AccessTokenClaims{}

	parsedToken, err := jwt.ParseWithClaims(
		rawToken,
		&claims,
		func(parsedToken *jwt.Token) (any, error) {
			if parsedToken.Method.Alg() != jwt.SigningMethodEdDSA.Alg() {
				return nil, fmt.Errorf(
					"unexpected access token signing method: %s",
					parsedToken.Method.Alg(),
				)
			}

			keyID, ok := parsedToken.Header["kid"].(string)
			if !ok || keyID == "" {
				return nil, errors.New("access token key ID is missing")
			}

			if keyID != v.keyID {
				return nil, errors.New("access token key ID is invalid")
			}

			return v.publicKey, nil
		},
		jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}),
		jwt.WithStrictDecoding(),
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(v.audience),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
	)
	if err != nil {
		return AccessTokenClaims{}, fmt.Errorf("verify access token: %w", err)
	}

	if parsedToken == nil || !parsedToken.Valid {
		return AccessTokenClaims{}, errors.New("access token is invalid")
	}

	if claims.Subject == "" {
		return AccessTokenClaims{}, errors.New("access token subject is missing")
	}

	if claims.SessionID == "" {
		return AccessTokenClaims{}, errors.New("access token session ID is missing")
	}

	return claims, nil
}

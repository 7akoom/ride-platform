package token

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestAccessTokenSignerIssueAndVerify(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate Ed25519 key pair: %v", err)
	}

	privateKeyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatalf("marshal private key: %v", err)
	}

	privateKeyPEM := pem.EncodeToMemory(
		&pem.Block{
			Type:  "PRIVATE KEY",
			Bytes: privateKeyDER,
		},
	)

	publicKeyDER, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}

	publicKeyPEM := pem.EncodeToMemory(
		&pem.Block{
			Type:  "PUBLIC KEY",
			Bytes: publicKeyDER,
		},
	)

	privateKeyPath := filepath.Join(
		t.TempDir(),
		"access_token_private.pem",
	)

	if err := os.WriteFile(
		privateKeyPath,
		privateKeyPEM,
		0600,
	); err != nil {
		t.Fatalf("write temporary private key: %v", err)
	}

	const (
		issuer     = "ride-identity"
		audience   = "ride-platform"
		keyID      = "identity-test-1"
		identityID = "identity-test-123"
		sessionID  = "session-test-456"
	)

	const ttl = 15 * time.Minute

	signer, err := NewAccessTokenSigner(
		privateKeyPath,
		issuer,
		audience,
		keyID,
		ttl,
	)
	if err != nil {
		t.Fatalf("NewAccessTokenSigner() returned an error: %v", err)
	}

	issuedAt := time.Now().UTC()

	signedToken, expiresInSeconds, err := signer.Issue(
		identityID,
		sessionID,
		issuedAt,
	)
	if err != nil {
		t.Fatalf("Issue() returned an error: %v", err)
	}

	if signedToken == "" {
		t.Fatal("Issue() returned an empty access token")
	}

	if expiresInSeconds != int32(ttl.Seconds()) {
		t.Fatalf(
			"Issue() returned expires_in=%d, expected %d",
			expiresInSeconds,
			int32(ttl.Seconds()),
		)
	}

	parsedPublicKey, err := jwt.ParseEdPublicKeyFromPEM(
		publicKeyPEM,
	)
	if err != nil {
		t.Fatalf("parse public key: %v", err)
	}

	var claims AccessTokenClaims

	parsedToken, err := jwt.ParseWithClaims(
		signedToken,
		&claims,
		func(token *jwt.Token) (any, error) {
			return parsedPublicKey, nil
		},
		jwt.WithValidMethods(
			[]string{jwt.SigningMethodEdDSA.Alg()},
		),
		jwt.WithIssuer(issuer),
		jwt.WithAudience(audience),
	)
	if err != nil {
		t.Fatalf("verify access token: %v", err)
	}

	if !parsedToken.Valid {
		t.Fatal("parsed access token is not valid")
	}

	if parsedToken.Method.Alg() != jwt.SigningMethodEdDSA.Alg() {
		t.Fatalf(
			"access token uses algorithm %q, expected %q",
			parsedToken.Method.Alg(),
			jwt.SigningMethodEdDSA.Alg(),
		)
	}

	kid, ok := parsedToken.Header["kid"].(string)
	if !ok {
		t.Fatal("access token does not contain string kid header")
	}

	if kid != keyID {
		t.Fatalf(
			"access token kid is %q, expected %q",
			kid,
			keyID,
		)
	}

	if claims.Subject != identityID {
		t.Fatalf(
			"access token subject is %q, expected %q",
			claims.Subject,
			identityID,
		)
	}

	if claims.SessionID != sessionID {
		t.Fatalf(
			"access token session ID is %q, expected %q",
			claims.SessionID,
			sessionID,
		)
	}

	if claims.Issuer != issuer {
		t.Fatalf(
			"access token issuer is %q, expected %q",
			claims.Issuer,
			issuer,
		)
	}

	if len(claims.Audience) != 1 ||
		claims.Audience[0] != audience {
		t.Fatalf(
			"access token audience is %v, expected [%s]",
			claims.Audience,
			audience,
		)
	}

	if claims.ID == "" {
		t.Fatal("access token has an empty jti")
	}

	if !strings.HasPrefix(claims.ID, "at_") {
		t.Fatalf(
			"access token jti %q does not have expected prefix",
			claims.ID,
		)
	}

	if claims.IssuedAt == nil {
		t.Fatal("access token has no issued-at claim")
	}

	if claims.ExpiresAt == nil {
		t.Fatal("access token has no expiration claim")
	}

	actualTTL := claims.ExpiresAt.Time.Unix() -
		claims.IssuedAt.Time.Unix()

	if actualTTL != int64(ttl.Seconds()) {
		t.Fatalf(
			"access token TTL is %d seconds, expected %d",
			actualTTL,
			int64(ttl.Seconds()),
		)
	}
}
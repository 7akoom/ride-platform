package token

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type testSessionAccessRevocationChecker struct {
	revoked   bool
	err       error
	called    bool
	sessionID string
}

func (c *testSessionAccessRevocationChecker) IsRevoked(
	ctx context.Context,
	sessionID string,
) (bool, error) {
	c.called = true
	c.sessionID = sessionID

	if c.err != nil {
		return false, c.err
	}

	return c.revoked, nil
}

func TestAccessTokenVerifierAcceptsValidNonRevokedToken(
	t *testing.T,
) {
	signer, verifier, checker := newAccessTokenVerifierTestSetup(
		t,
	)

	const (
		identityID = "identity-test-123"
		sessionID  = "session-test-456"
	)

	issuedAt := time.Now().
		UTC().
		Truncate(time.Second)

	sessionExpiresAt := issuedAt.Add(
		24 * time.Hour,
	)

	signedToken, _, err := signer.IssueForSession(
		identityID,
		sessionID,
		issuedAt,
		sessionExpiresAt,
	)
	if err != nil {
		t.Fatalf(
			"IssueForSession() returned an error: %v",
			err,
		)
	}

	claims, err := verifier.Verify(
		context.Background(),
		signedToken,
	)
	if err != nil {
		t.Fatalf(
			"Verify() returned an error: %v",
			err,
		)
	}

	if claims.Subject != identityID {
		t.Fatalf(
			"verified subject = %q, expected %q",
			claims.Subject,
			identityID,
		)
	}

	if claims.SessionID != sessionID {
		t.Fatalf(
			"verified session ID = %q, expected %q",
			claims.SessionID,
			sessionID,
		)
	}

	if !checker.called {
		t.Fatal(
			"session revocation checker was not called",
		)
	}

	if checker.sessionID != sessionID {
		t.Fatalf(
			"revocation checker session ID = %q, expected %q",
			checker.sessionID,
			sessionID,
		)
	}
}

func TestAccessTokenVerifierRejectsRevokedSession(
	t *testing.T,
) {
	signer, verifier, checker := newAccessTokenVerifierTestSetup(
		t,
	)

	const (
		identityID = "identity-test-123"
		sessionID  = "session-test-456"
	)

	issuedAt := time.Now().
		UTC().
		Truncate(time.Second)

	sessionExpiresAt := issuedAt.Add(
		24 * time.Hour,
	)

	signedToken, _, err := signer.IssueForSession(
		identityID,
		sessionID,
		issuedAt,
		sessionExpiresAt,
	)
	if err != nil {
		t.Fatalf(
			"IssueForSession() returned an error: %v",
			err,
		)
	}

	checker.revoked = true

	_, err = verifier.Verify(
		context.Background(),
		signedToken,
	)
	if err == nil {
		t.Fatal(
			"Verify() returned nil error for revoked session",
		)
	}

	if !errors.Is(err, ErrAccessTokenRevoked) {
		t.Fatalf(
			"Verify() error = %v, expected %v",
			err,
			ErrAccessTokenRevoked,
		)
	}

	if !checker.called {
		t.Fatal(
			"session revocation checker was not called",
		)
	}

	if checker.sessionID != sessionID {
		t.Fatalf(
			"revocation checker session ID = %q, expected %q",
			checker.sessionID,
			sessionID,
		)
	}
}

func TestAccessTokenVerifierFailsClosedWhenRevocationCheckFails(
	t *testing.T,
) {
	signer, verifier, checker := newAccessTokenVerifierTestSetup(
		t,
	)

	expectedErr := errors.New(
		"Valkey unavailable",
	)

	checker.err = expectedErr

	const (
		identityID = "identity-test-123"
		sessionID  = "session-test-456"
	)

	issuedAt := time.Now().
		UTC().
		Truncate(time.Second)

	sessionExpiresAt := issuedAt.Add(
		24 * time.Hour,
	)

	signedToken, _, err := signer.IssueForSession(
		identityID,
		sessionID,
		issuedAt,
		sessionExpiresAt,
	)
	if err != nil {
		t.Fatalf(
			"IssueForSession() returned an error: %v",
			err,
		)
	}

	_, err = verifier.Verify(
		context.Background(),
		signedToken,
	)
	if err == nil {
		t.Fatal(
			"Verify() returned nil error when revocation check failed",
		)
	}

	if !errors.Is(err, expectedErr) {
		t.Fatalf(
			"Verify() error = %v, expected wrapped %v",
			err,
			expectedErr,
		)
	}

	if !checker.called {
		t.Fatal(
			"session revocation checker was not called",
		)
	}
}

func TestAccessTokenVerifierRejectsNonCanonicalBase64URLSignature(
	t *testing.T,
) {
	signer, verifier, checker := newAccessTokenVerifierTestSetup(
		t,
	)

	issuedAt := time.Now().
		UTC().
		Truncate(time.Second)

	signedToken, _, err := signer.IssueForSession(
		"identity-test-123",
		"session-test-456",
		issuedAt,
		issuedAt.Add(24*time.Hour),
	)
	if err != nil {
		t.Fatalf(
			"IssueForSession() returned an error: %v",
			err,
		)
	}

	parts := strings.Split(
		signedToken,
		".",
	)

	if len(parts) != 3 {
		t.Fatalf(
			"signed token contains %d segments, expected 3",
			len(parts),
		)
	}

	signature := parts[2]

	if signature == "" {
		t.Fatal(
			"signed token contains an empty signature",
		)
	}

	const base64URLAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"

	lastCharacterIndex := strings.IndexByte(
		base64URLAlphabet,
		signature[len(signature)-1],
	)

	if lastCharacterIndex < 0 {
		t.Fatal(
			"signature contains an invalid Base64URL character",
		)
	}

	// An Ed25519 signature is 64 bytes. Its unpadded Base64URL
	// representation ends with four unused trailing bits.
	// Canonical encoding requires those bits to be zero.
	//
	// Changing only one of those unused bits preserves the decoded
	// signature bytes while making the Base64URL representation
	// non-canonical.
	if lastCharacterIndex%16 != 0 {
		t.Fatalf(
			"signature final Base64URL character index = %d, expected canonical trailing bits",
			lastCharacterIndex,
		)
	}

	mutatedSignature := []byte(signature)

	mutatedSignature[len(mutatedSignature)-1] =
		base64URLAlphabet[lastCharacterIndex+1]

	parts[2] = string(mutatedSignature)

	nonCanonicalToken := strings.Join(
		parts,
		".",
	)

	_, err = verifier.Verify(
		context.Background(),
		nonCanonicalToken,
	)

	if err == nil {
		t.Fatal(
			"Verify() accepted a token with non-canonical Base64URL signature encoding",
		)
	}

	if checker.called {
		t.Fatal(
			"session revocation checker was called for malformed access token",
		)
	}
}

func newAccessTokenVerifierTestSetup(
	t *testing.T,
) (
	*AccessTokenSigner,
	*AccessTokenVerifier,
	*testSessionAccessRevocationChecker,
) {
	t.Helper()

	publicKey, privateKey, err := ed25519.GenerateKey(
		rand.Reader,
	)
	if err != nil {
		t.Fatalf(
			"generate Ed25519 key pair: %v",
			err,
		)
	}

	privateKeyDER, err := x509.MarshalPKCS8PrivateKey(
		privateKey,
	)
	if err != nil {
		t.Fatalf(
			"marshal private key: %v",
			err,
		)
	}

	privateKeyPEM := pem.EncodeToMemory(
		&pem.Block{
			Type:  "PRIVATE KEY",
			Bytes: privateKeyDER,
		},
	)

	publicKeyDER, err := x509.MarshalPKIXPublicKey(
		publicKey,
	)
	if err != nil {
		t.Fatalf(
			"marshal public key: %v",
			err,
		)
	}

	publicKeyPEM := pem.EncodeToMemory(
		&pem.Block{
			Type:  "PUBLIC KEY",
			Bytes: publicKeyDER,
		},
	)

	tempDir := t.TempDir()

	privateKeyPath := filepath.Join(
		tempDir,
		"access_token_private.pem",
	)

	publicKeyPath := filepath.Join(
		tempDir,
		"access_token_public.pem",
	)

	if err := os.WriteFile(
		privateKeyPath,
		privateKeyPEM,
		0600,
	); err != nil {
		t.Fatalf(
			"write temporary private key: %v",
			err,
		)
	}

	if err := os.WriteFile(
		publicKeyPath,
		publicKeyPEM,
		0600,
	); err != nil {
		t.Fatalf(
			"write temporary public key: %v",
			err,
		)
	}

	const (
		issuer   = "ride-identity"
		audience = "ride-platform"
		keyID    = "identity-test-1"
	)

	const accessTokenTTL = 15 * time.Minute

	signer, err := NewAccessTokenSigner(
		privateKeyPath,
		issuer,
		audience,
		keyID,
		accessTokenTTL,
	)
	if err != nil {
		t.Fatalf(
			"NewAccessTokenSigner() returned an error: %v",
			err,
		)
	}

	checker := &testSessionAccessRevocationChecker{}

	verifier, err := NewAccessTokenVerifier(
		publicKeyPath,
		issuer,
		audience,
		keyID,
		checker,
	)
	if err != nil {
		t.Fatalf(
			"NewAccessTokenVerifier() returned an error: %v",
			err,
		)
	}

	return signer, verifier, checker
}

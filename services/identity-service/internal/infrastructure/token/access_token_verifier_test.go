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

type testAccessTokenVerificationMetricsRecorder struct {
	records []testAccessTokenVerificationMetric
}

type testAccessTokenVerificationMetric struct {
	outcome  AccessTokenVerificationMetricOutcome
	duration time.Duration
}

func (r *testAccessTokenVerificationMetricsRecorder) RecordAccessTokenVerification(
	_ context.Context,
	outcome AccessTokenVerificationMetricOutcome,
	duration time.Duration,
) {
	r.records = append(
		r.records,
		testAccessTokenVerificationMetric{
			outcome:  outcome,
			duration: duration,
		},
	)
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

	metricsRecorder :=
		&testAccessTokenVerificationMetricsRecorder{}

	verifier.metricsRecorder = metricsRecorder

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
		"",
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
	requireSingleAccessTokenVerificationMetric(
		t,
		metricsRecorder,
		AccessTokenVerificationMetricOutcomeSuccess,
	)
}

func TestAccessTokenVerifierRejectsRevokedSession(
	t *testing.T,
) {
	signer, verifier, checker := newAccessTokenVerifierTestSetup(
		t,
	)

	metricsRecorder :=
		&testAccessTokenVerificationMetricsRecorder{}

	verifier.metricsRecorder = metricsRecorder

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
		"",
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

	requireSingleAccessTokenVerificationMetric(
		t,
		metricsRecorder,
		AccessTokenVerificationMetricOutcomeRejected,
	)
}

func TestAccessTokenVerifierFailsClosedWhenRevocationCheckFails(
	t *testing.T,
) {
	signer, verifier, checker := newAccessTokenVerifierTestSetup(
		t,
	)

	metricsRecorder :=
		&testAccessTokenVerificationMetricsRecorder{}

	verifier.metricsRecorder = metricsRecorder

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
		"",
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

	requireSingleAccessTokenVerificationMetric(
		t,
		metricsRecorder,
		AccessTokenVerificationMetricOutcomeFailed,
	)
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
		"",
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

type accessTokenVerifierTestKeyPair struct {
	keyID          string
	privateKeyPath string
	publicKeyPath  string
}

func TestAccessTokenVerifierKeyringAcceptsCurrentAndPreviousKeys(
	t *testing.T,
) {
	const (
		issuer   = "ride-identity"
		audience = "ride-platform"
	)

	currentKey := newAccessTokenVerifierTestKeyPair(
		t,
		"identity-current",
	)

	previousKey := newAccessTokenVerifierTestKeyPair(
		t,
		"identity-previous",
	)

	checker := &testSessionAccessRevocationChecker{}

	verifier, err := NewAccessTokenVerifierWithKeyring(
		[]AccessTokenVerificationKey{
			{
				KeyID:         currentKey.keyID,
				PublicKeyPath: currentKey.publicKeyPath,
			},
			{
				KeyID:         previousKey.keyID,
				PublicKeyPath: previousKey.publicKeyPath,
			},
		},
		issuer,
		audience,
		checker,
	)
	if err != nil {
		t.Fatalf(
			"NewAccessTokenVerifierWithKeyring() returned an error: %v",
			err,
		)
	}

	issuedAt := time.Now().
		UTC().
		Truncate(time.Second)

	sessionExpiresAt := issuedAt.Add(
		24 * time.Hour,
	)

	tests := []struct {
		name      string
		key       accessTokenVerifierTestKeyPair
		sessionID string
	}{
		{
			name:      "current key",
			key:       currentKey,
			sessionID: "session-current",
		},
		{
			name:      "previous key",
			key:       previousKey,
			sessionID: "session-previous",
		},
	}

	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				signer, err := NewAccessTokenSigner(
					test.key.privateKeyPath,
					issuer,
					audience,
					test.key.keyID,
					15*time.Minute,
				)
				if err != nil {
					t.Fatalf(
						"NewAccessTokenSigner() returned an error: %v",
						err,
					)
				}

				signedToken, _, err :=
					signer.IssueForSession(
						"identity-test-rotation",
						test.sessionID,
						"",
						issuedAt,
						sessionExpiresAt,
					)
				if err != nil {
					t.Fatalf(
						"IssueForSession() returned an error: %v",
						err,
					)
				}

				checker.called = false
				checker.sessionID = ""

				claims, err := verifier.Verify(
					context.Background(),
					signedToken,
				)
				if err != nil {
					t.Fatalf(
						"Verify() returned an error for %s: %v",
						test.name,
						err,
					)
				}

				if claims.SessionID != test.sessionID {
					t.Fatalf(
						"verified session ID = %q, expected %q",
						claims.SessionID,
						test.sessionID,
					)
				}

				if !checker.called {
					t.Fatal(
						"session revocation checker was not called",
					)
				}

				if checker.sessionID != test.sessionID {
					t.Fatalf(
						"revocation checker session ID = %q, expected %q",
						checker.sessionID,
						test.sessionID,
					)
				}
			},
		)
	}
}

func TestAccessTokenVerifierKeyringRejectsUnknownKeyID(
	t *testing.T,
) {
	const (
		issuer   = "ride-identity"
		audience = "ride-platform"
	)

	currentKey := newAccessTokenVerifierTestKeyPair(
		t,
		"identity-current",
	)

	unknownKey := newAccessTokenVerifierTestKeyPair(
		t,
		"identity-unknown",
	)

	checker := &testSessionAccessRevocationChecker{}

	verifier, err := NewAccessTokenVerifierWithKeyring(
		[]AccessTokenVerificationKey{
			{
				KeyID:         currentKey.keyID,
				PublicKeyPath: currentKey.publicKeyPath,
			},
		},
		issuer,
		audience,
		checker,
	)
	if err != nil {
		t.Fatalf(
			"NewAccessTokenVerifierWithKeyring() returned an error: %v",
			err,
		)
	}

	signer, err := NewAccessTokenSigner(
		unknownKey.privateKeyPath,
		issuer,
		audience,
		unknownKey.keyID,
		15*time.Minute,
	)
	if err != nil {
		t.Fatalf(
			"NewAccessTokenSigner() returned an error: %v",
			err,
		)
	}

	issuedAt := time.Now().
		UTC().
		Truncate(time.Second)

	signedToken, _, err := signer.IssueForSession(
		"identity-test-rotation",
		"session-unknown-kid",
		"",
		issuedAt,
		issuedAt.Add(24*time.Hour),
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
			"Verify() accepted an access token with unknown key ID",
		)
	}

	if !strings.Contains(
		err.Error(),
		"access token key ID is invalid",
	) {
		t.Fatalf(
			"Verify() error = %v, expected invalid key ID error",
			err,
		)
	}

	if checker.called {
		t.Fatal(
			"session revocation checker was called for token with unknown key ID",
		)
	}
}

func TestAccessTokenVerifierKeyringRejectsWrongSignatureForKnownKeyID(
	t *testing.T,
) {
	const (
		issuer   = "ride-identity"
		audience = "ride-platform"
	)

	currentKey := newAccessTokenVerifierTestKeyPair(
		t,
		"identity-current",
	)

	wrongSigningKey := newAccessTokenVerifierTestKeyPair(
		t,
		"identity-wrong-signature",
	)

	checker := &testSessionAccessRevocationChecker{}

	verifier, err := NewAccessTokenVerifierWithKeyring(
		[]AccessTokenVerificationKey{
			{
				KeyID:         currentKey.keyID,
				PublicKeyPath: currentKey.publicKeyPath,
			},
		},
		issuer,
		audience,
		checker,
	)
	if err != nil {
		t.Fatalf(
			"NewAccessTokenVerifierWithKeyring() returned an error: %v",
			err,
		)
	}

	signer, err := NewAccessTokenSigner(
		wrongSigningKey.privateKeyPath,
		issuer,
		audience,
		currentKey.keyID,
		15*time.Minute,
	)
	if err != nil {
		t.Fatalf(
			"NewAccessTokenSigner() returned an error: %v",
			err,
		)
	}

	issuedAt := time.Now().
		UTC().
		Truncate(time.Second)

	signedToken, _, err := signer.IssueForSession(
		"identity-test-rotation",
		"session-wrong-signature",
		"",
		issuedAt,
		issuedAt.Add(24*time.Hour),
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
			"Verify() accepted token signed by the wrong private key for known key ID",
		)
	}

	if checker.called {
		t.Fatal(
			"session revocation checker was called for token with invalid signature",
		)
	}
}

func TestAccessTokenVerifierRejectsRetiredKeyAfterRemovalFromKeyring(
	t *testing.T,
) {
	const (
		issuer   = "ride-identity"
		audience = "ride-platform"
	)

	currentKey := newAccessTokenVerifierTestKeyPair(
		t,
		"identity-current",
	)

	retiredKey := newAccessTokenVerifierTestKeyPair(
		t,
		"identity-retired",
	)

	signer, err := NewAccessTokenSigner(
		retiredKey.privateKeyPath,
		issuer,
		audience,
		retiredKey.keyID,
		15*time.Minute,
	)
	if err != nil {
		t.Fatalf(
			"NewAccessTokenSigner() returned an error: %v",
			err,
		)
	}

	issuedAt := time.Now().
		UTC().
		Truncate(time.Second)

	signedToken, _, err := signer.IssueForSession(
		"identity-test-rotation",
		"session-retired-key",
		"",
		issuedAt,
		issuedAt.Add(24*time.Hour),
	)
	if err != nil {
		t.Fatalf(
			"IssueForSession() returned an error: %v",
			err,
		)
	}

	checker := &testSessionAccessRevocationChecker{}

	verifier, err := NewAccessTokenVerifierWithKeyring(
		[]AccessTokenVerificationKey{
			{
				KeyID:         currentKey.keyID,
				PublicKeyPath: currentKey.publicKeyPath,
			},
		},
		issuer,
		audience,
		checker,
	)
	if err != nil {
		t.Fatalf(
			"NewAccessTokenVerifierWithKeyring() returned an error: %v",
			err,
		)
	}

	_, err = verifier.Verify(
		context.Background(),
		signedToken,
	)

	if err == nil {
		t.Fatal(
			"Verify() accepted access token signed by retired key",
		)
	}

	if !strings.Contains(
		err.Error(),
		"access token key ID is invalid",
	) {
		t.Fatalf(
			"Verify() error = %v, expected retired key ID rejection",
			err,
		)
	}

	if checker.called {
		t.Fatal(
			"session revocation checker was called for token signed by retired key",
		)
	}
}

func TestNewAccessTokenVerifierWithKeyringRejectsInvalidKeyring(
	t *testing.T,
) {
	const (
		issuer   = "ride-identity"
		audience = "ride-platform"
	)

	validKey := newAccessTokenVerifierTestKeyPair(
		t,
		"identity-valid",
	)

	checker := &testSessionAccessRevocationChecker{}

	tests := []struct {
		name          string
		keys          []AccessTokenVerificationKey
		expectedError string
	}{
		{
			name:          "empty keyring",
			keys:          nil,
			expectedError: "access token verification keyring cannot be empty",
		},
		{
			name: "blank key ID",
			keys: []AccessTokenVerificationKey{
				{
					KeyID:         "",
					PublicKeyPath: validKey.publicKeyPath,
				},
			},
			expectedError: "access token verification key ID cannot be empty",
		},
		{
			name: "blank public key path",
			keys: []AccessTokenVerificationKey{
				{
					KeyID:         "identity-missing-path",
					PublicKeyPath: "",
				},
			},
			expectedError: "access token public key path cannot be empty",
		},
		{
			name: "duplicate key ID",
			keys: []AccessTokenVerificationKey{
				{
					KeyID:         validKey.keyID,
					PublicKeyPath: validKey.publicKeyPath,
				},
				{
					KeyID:         validKey.keyID,
					PublicKeyPath: validKey.publicKeyPath,
				},
			},
			expectedError: "duplicate access token verification key ID",
		},
	}

	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				_, err :=
					NewAccessTokenVerifierWithKeyring(
						test.keys,
						issuer,
						audience,
						checker,
					)

				if err == nil {
					t.Fatal(
						"NewAccessTokenVerifierWithKeyring() returned nil error",
					)
				}

				if !strings.Contains(
					err.Error(),
					test.expectedError,
				) {
					t.Fatalf(
						"error = %v, expected error containing %q",
						err,
						test.expectedError,
					)
				}
			},
		)
	}
}

func newAccessTokenVerifierTestKeyPair(
	t *testing.T,
	keyID string,
) accessTokenVerifierTestKeyPair {
	t.Helper()

	publicKey, privateKey, err :=
		ed25519.GenerateKey(
			rand.Reader,
		)
	if err != nil {
		t.Fatalf(
			"generate Ed25519 key pair: %v",
			err,
		)
	}

	privateKeyDER, err :=
		x509.MarshalPKCS8PrivateKey(
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

	publicKeyDER, err :=
		x509.MarshalPKIXPublicKey(
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
		"private.pem",
	)

	publicKeyPath := filepath.Join(
		tempDir,
		"public.pem",
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

	return accessTokenVerifierTestKeyPair{
		keyID:          keyID,
		privateKeyPath: privateKeyPath,
		publicKeyPath:  publicKeyPath,
	}
}

func TestAccessTokenSigningKeyRotationSequence(
	t *testing.T,
) {
	const (
		issuer   = "ride-identity"
		audience = "ride-platform"
	)

	oldKey := newAccessTokenVerifierTestKeyPair(
		t,
		"identity-old",
	)

	newKey := newAccessTokenVerifierTestKeyPair(
		t,
		"identity-new",
	)

	oldSigner, err := NewAccessTokenSigner(
		oldKey.privateKeyPath,
		issuer,
		audience,
		oldKey.keyID,
		15*time.Minute,
	)
	if err != nil {
		t.Fatalf(
			"create old access token signer: %v",
			err,
		)
	}

	newSigner, err := NewAccessTokenSigner(
		newKey.privateKeyPath,
		issuer,
		audience,
		newKey.keyID,
		15*time.Minute,
	)
	if err != nil {
		t.Fatalf(
			"create new access token signer: %v",
			err,
		)
	}

	issuedAt := time.Now().
		UTC().
		Truncate(time.Second)

	sessionExpiresAt := issuedAt.Add(
		24 * time.Hour,
	)

	oldToken, _, err := oldSigner.IssueForSession(
		"identity-rotation-test",
		"session-old-token",
		"",
		issuedAt,
		sessionExpiresAt,
	)
	if err != nil {
		t.Fatalf(
			"issue old-key access token: %v",
			err,
		)
	}

	t.Run(
		"old key only accepts old token",
		func(t *testing.T) {
			checker :=
				&testSessionAccessRevocationChecker{}

			verifier, err :=
				NewAccessTokenVerifierWithKeyring(
					[]AccessTokenVerificationKey{
						{
							KeyID:         oldKey.keyID,
							PublicKeyPath: oldKey.publicKeyPath,
						},
					},
					issuer,
					audience,
					checker,
				)
			if err != nil {
				t.Fatalf(
					"create old-only verifier: %v",
					err,
				)
			}

			_, err = verifier.Verify(
				context.Background(),
				oldToken,
			)
			if err != nil {
				t.Fatalf(
					"old-only verifier rejected old token: %v",
					err,
				)
			}
		},
	)

	var newToken string

	t.Run(
		"overlap accepts old and new tokens",
		func(t *testing.T) {
			checker :=
				&testSessionAccessRevocationChecker{}

			verifier, err :=
				NewAccessTokenVerifierWithKeyring(
					[]AccessTokenVerificationKey{
						{
							KeyID:         oldKey.keyID,
							PublicKeyPath: oldKey.publicKeyPath,
						},
						{
							KeyID:         newKey.keyID,
							PublicKeyPath: newKey.publicKeyPath,
						},
					},
					issuer,
					audience,
					checker,
				)
			if err != nil {
				t.Fatalf(
					"create overlap verifier: %v",
					err,
				)
			}

			_, err = verifier.Verify(
				context.Background(),
				oldToken,
			)
			if err != nil {
				t.Fatalf(
					"overlap verifier rejected old token: %v",
					err,
				)
			}

			newToken, _, err =
				newSigner.IssueForSession(
					"identity-rotation-test",
					"session-new-token",
					"",
					issuedAt,
					sessionExpiresAt,
				)
			if err != nil {
				t.Fatalf(
					"issue new-key access token: %v",
					err,
				)
			}

			checker.called = false
			checker.sessionID = ""

			_, err = verifier.Verify(
				context.Background(),
				newToken,
			)
			if err != nil {
				t.Fatalf(
					"overlap verifier rejected new token: %v",
					err,
				)
			}
		},
	)

	t.Run(
		"retiring old key rejects old token and keeps new token valid",
		func(t *testing.T) {
			checker :=
				&testSessionAccessRevocationChecker{}

			verifier, err :=
				NewAccessTokenVerifierWithKeyring(
					[]AccessTokenVerificationKey{
						{
							KeyID:         newKey.keyID,
							PublicKeyPath: newKey.publicKeyPath,
						},
					},
					issuer,
					audience,
					checker,
				)
			if err != nil {
				t.Fatalf(
					"create new-only verifier: %v",
					err,
				)
			}

			_, err = verifier.Verify(
				context.Background(),
				newToken,
			)
			if err != nil {
				t.Fatalf(
					"new-only verifier rejected new token: %v",
					err,
				)
			}

			checker.called = false
			checker.sessionID = ""

			_, err = verifier.Verify(
				context.Background(),
				oldToken,
			)
			if err == nil {
				t.Fatal(
					"new-only verifier accepted token signed by retired key",
				)
			}

			if !strings.Contains(
				err.Error(),
				"access token key ID is invalid",
			) {
				t.Fatalf(
					"retired old-key token error = %v, expected invalid key ID rejection",
					err,
				)
			}

			if checker.called {
				t.Fatal(
					"session revocation checker was called for token signed by retired key",
				)
			}
		},
	)
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
func requireSingleAccessTokenVerificationMetric(
	t *testing.T,
	recorder *testAccessTokenVerificationMetricsRecorder,
	expectedOutcome AccessTokenVerificationMetricOutcome,
) {
	t.Helper()

	if len(recorder.records) != 1 {
		t.Fatalf(
			"access token verification metric count = %d, expected 1",
			len(recorder.records),
		)
	}

	record := recorder.records[0]

	if record.outcome != expectedOutcome {
		t.Fatalf(
			"access token verification outcome = %q, expected %q",
			record.outcome,
			expectedOutcome,
		)
	}

	if record.duration <= 0 {
		t.Fatalf(
			"access token verification duration = %v, expected positive duration",
			record.duration,
		)
	}
}
func TestAccessTokenVerifierRecordsRejectedMetricForMalformedToken(
	t *testing.T,
) {
	_, verifier, _ := newAccessTokenVerifierTestSetup(
		t,
	)

	metricsRecorder :=
		&testAccessTokenVerificationMetricsRecorder{}

	verifier.metricsRecorder = metricsRecorder

	_, err := verifier.Verify(
		context.Background(),
		"not-a-valid-jwt",
	)
	if err == nil {
		t.Fatal(
			"Verify() returned nil error for malformed token",
		)
	}

	requireSingleAccessTokenVerificationMetric(
		t,
		metricsRecorder,
		AccessTokenVerificationMetricOutcomeRejected,
	)
}

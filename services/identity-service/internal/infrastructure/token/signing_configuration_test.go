package token

import (
	"testing"
	"time"
)

func TestValidateSignerMatchesActiveVerificationKey(t *testing.T) {
	current := newAccessTokenVerifierTestKeyPair(t, "current")
	previous := newAccessTokenVerifierTestKeyPair(t, "previous")
	signer, err := NewAccessTokenSigner(current.privateKeyPath, "issuer", "audience", "current", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, activeKeyPath, issuer, audience, keyID string
		wantErr                                      bool
	}{
		{"matching", current.publicKeyPath, "issuer", "audience", "current", false},
		{"mismatched_key", previous.publicKeyPath, "issuer", "audience", "current", true},
		{"missing_active_kid", current.publicKeyPath, "issuer", "audience", "other", true},
		{"issuer_mismatch", current.publicKeyPath, "different", "audience", "current", true},
		{"audience_mismatch", current.publicKeyPath, "issuer", "different", "current", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			verifier, err := NewAccessTokenVerifierWithKeyring([]AccessTokenVerificationKey{
				{KeyID: tc.keyID, PublicKeyPath: tc.activeKeyPath},
				{KeyID: "previous", PublicKeyPath: previous.publicKeyPath},
			}, tc.issuer, tc.audience, &testSessionAccessRevocationChecker{})
			if err != nil {
				t.Fatal(err)
			}
			if err := verifier.ValidateSigner(signer); (err != nil) != tc.wantErr {
				t.Fatalf("ValidateSigner() error presence = %t, want %t", err != nil, tc.wantErr)
			}
		})
	}
}

func TestValidateSignerRejectsUnconfiguredComponents(t *testing.T) {
	var verifier *AccessTokenVerifier
	if verifier.ValidateSigner(&AccessTokenSigner{}) == nil {
		t.Fatal("expected nil verifier rejection")
	}
	verifier = &AccessTokenVerifier{}
	if verifier.ValidateSigner(nil) == nil {
		t.Fatal("expected nil signer rejection")
	}
	if verifier.ValidateSigner(&AccessTokenSigner{}) == nil {
		t.Fatal("expected unconfigured key rejection")
	}
}

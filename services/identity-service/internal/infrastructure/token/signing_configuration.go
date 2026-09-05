package token

import (
	"crypto/ed25519"
	"errors"
)

func (v *AccessTokenVerifier) ValidateSigner(signer *AccessTokenSigner) error {
	if v == nil || signer == nil {
		return errors.New("access token signer and verifier are required")
	}
	if len(signer.privateKey) != ed25519.PrivateKeySize {
		return errors.New("access token signing private key is not configured")
	}
	publicKey, exists := v.publicKeys[signer.keyID]
	if !exists {
		return errors.New("active signing key ID is absent from the verification keyring")
	}
	if !publicKey.Equal(signer.privateKey.Public()) {
		return errors.New("active verification key does not match the signing private key")
	}
	if v.issuer != signer.issuer || v.audience != signer.audience {
		return errors.New("access token signer and verifier issuer/audience must match")
	}
	return nil
}

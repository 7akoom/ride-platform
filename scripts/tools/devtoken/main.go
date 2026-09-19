// devtoken mints a short-lived access token for local end-to-end tests,
// signed with the LOCAL development key of identity-service. Never use it
// outside a development machine. It prints the token and nothing else.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"os"
	"time"
)

func main() {
	subject := flag.String("sub", "", "identity id to put in the token (required)")
	keyPath := flag.String("key", "services/identity-service/.local/keys/access_token_private.pem", "Ed25519 private key (PEM, PKCS8)")
	keyID := flag.String("kid", envOr("ACCESS_TOKEN_KEY_ID", "identity-dev-1"), "key id header")
	issuer := flag.String("iss", envOr("ACCESS_TOKEN_ISSUER", "ride-identity"), "issuer")
	audience := flag.String("aud", envOr("ACCESS_TOKEN_AUDIENCE", "ride-platform"), "audience")
	ttl := flag.Duration("ttl", 10*time.Minute, "token lifetime; a negative value mints an already expired token")
	flag.Parse()

	if *subject == "" {
		fail("-sub is required")
	}

	pemBytes, err := os.ReadFile(*keyPath)
	if err != nil {
		fail("cannot read the signing key: " + err.Error())
	}

	block, _ := pem.Decode(pemBytes)
	if block == nil {
		fail("the signing key is not PEM")
	}

	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		fail("cannot parse the signing key: " + err.Error())
	}

	key, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		fail("the signing key is not Ed25519")
	}

	token, err := mint(key, *keyID, *issuer, *audience, *subject, *ttl, time.Now())
	if err != nil {
		fail(err.Error())
	}

	fmt.Println(token)
}

func mint(key ed25519.PrivateKey, keyID, issuer, audience, subject string, ttl time.Duration, now time.Time) (string, error) {
	header, err := json.Marshal(map[string]string{"alg": "EdDSA", "typ": "JWT", "kid": keyID})
	if err != nil {
		return "", err
	}

	jti := make([]byte, 16)
	if _, err := rand.Read(jti); err != nil {
		return "", err
	}

	claims, err := json.Marshal(map[string]any{
		"iss": issuer,
		"aud": []string{audience},
		"sub": subject,
		"sid": "devtoken-" + hex.EncodeToString(jti[:4]),
		"jti": hex.EncodeToString(jti),
		"iat": now.Unix(),
		"nbf": now.Add(-time.Minute).Unix(),
		"exp": now.Add(ttl).Unix(),
	})
	if err != nil {
		return "", err
	}

	encode := base64.RawURLEncoding.EncodeToString
	signingInput := encode(header) + "." + encode(claims)

	return signingInput + "." + encode(ed25519.Sign(key, []byte(signingInput))), nil
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}

	return fallback
}

func fail(message string) {
	fmt.Fprintln(os.Stderr, "devtoken:", message)
	os.Exit(1)
}

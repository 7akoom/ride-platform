package main

import (
	"strings"
	"testing"
)

func TestParseAccessTokenVerificationKeysUsesLegacyFallback(
	t *testing.T,
) {
	keys, err := parseAccessTokenVerificationKeys(
		"",
		" /run/secrets/current.pub ",
		" identity-current ",
	)
	if err != nil {
		t.Fatalf(
			"parseAccessTokenVerificationKeys() returned an error: %v",
			err,
		)
	}

	if len(keys) != 1 {
		t.Fatalf(
			"verification key count = %d, expected 1",
			len(keys),
		)
	}

	if keys[0].KeyID != "identity-current" {
		t.Fatalf(
			"verification key ID = %q, expected %q",
			keys[0].KeyID,
			"identity-current",
		)
	}

	if keys[0].PublicKeyPath != "/run/secrets/current.pub" {
		t.Fatalf(
			"verification public key path = %q, expected %q",
			keys[0].PublicKeyPath,
			"/run/secrets/current.pub",
		)
	}
}

func TestParseAccessTokenVerificationKeysParsesMultiKeyKeyring(
	t *testing.T,
) {
	rawKeyring := `
		[
			{
				"kid": "identity-current",
				"public_key_path": "/run/secrets/current.pub"
			},
			{
				"kid": "identity-previous",
				"public_key_path": "/run/secrets/previous.pub"
			}
		]
	`

	keys, err := parseAccessTokenVerificationKeys(
		rawKeyring,
		"/run/secrets/legacy.pub",
		"identity-current",
	)
	if err != nil {
		t.Fatalf(
			"parseAccessTokenVerificationKeys() returned an error: %v",
			err,
		)
	}

	if len(keys) != 2 {
		t.Fatalf(
			"verification key count = %d, expected 2",
			len(keys),
		)
	}

	if keys[0].KeyID != "identity-current" {
		t.Fatalf(
			"first verification key ID = %q, expected %q",
			keys[0].KeyID,
			"identity-current",
		)
	}

	if keys[0].PublicKeyPath != "/run/secrets/current.pub" {
		t.Fatalf(
			"first verification public key path = %q, expected %q",
			keys[0].PublicKeyPath,
			"/run/secrets/current.pub",
		)
	}

	if keys[1].KeyID != "identity-previous" {
		t.Fatalf(
			"second verification key ID = %q, expected %q",
			keys[1].KeyID,
			"identity-previous",
		)
	}

	if keys[1].PublicKeyPath != "/run/secrets/previous.pub" {
		t.Fatalf(
			"second verification public key path = %q, expected %q",
			keys[1].PublicKeyPath,
			"/run/secrets/previous.pub",
		)
	}
}

func TestParseAccessTokenVerificationKeysRejectsInvalidConfiguration(
	t *testing.T,
) {
	tests := []struct {
		name                string
		rawKeyring          string
		legacyPublicKeyPath string
		activeKeyID         string
		expectedError       string
	}{
		{
			name:                "blank active key ID",
			rawKeyring:          "",
			legacyPublicKeyPath: "/run/secrets/current.pub",
			activeKeyID:         "   ",
			expectedError:       "access token active key ID cannot be empty",
		},
		{
			name:                "legacy fallback missing public key",
			rawKeyring:          "",
			legacyPublicKeyPath: "   ",
			activeKeyID:         "identity-current",
			expectedError:       "access token public key path cannot be empty",
		},
		{
			name:          "malformed JSON",
			rawKeyring:    `[{"kid":"identity-current"`,
			activeKeyID:   "identity-current",
			expectedError: "decode access token verification keyring",
		},
		{
			name:          "wrong JSON type",
			rawKeyring:    `{"kid":"identity-current","public_key_path":"/current.pub"}`,
			activeKeyID:   "identity-current",
			expectedError: "decode access token verification keyring",
		},
		{
			name:          "empty keyring",
			rawKeyring:    `[]`,
			activeKeyID:   "identity-current",
			expectedError: "access token verification keyring cannot be empty",
		},
		{
			name: "unknown JSON field",
			rawKeyring: `
				[
					{
						"kid": "identity-current",
						"public_key_path": "/current.pub",
						"unexpected": true
					}
				]
			`,
			activeKeyID:   "identity-current",
			expectedError: "decode access token verification keyring",
		},
		{
			name: "blank key ID",
			rawKeyring: `
				[
					{
						"kid": "   ",
						"public_key_path": "/current.pub"
					}
				]
			`,
			activeKeyID:   "identity-current",
			expectedError: "has an empty key ID",
		},
		{
			name: "blank public key path",
			rawKeyring: `
				[
					{
						"kid": "identity-current",
						"public_key_path": "   "
					}
				]
			`,
			activeKeyID:   "identity-current",
			expectedError: "has an empty public key path",
		},
		{
			name: "duplicate key ID",
			rawKeyring: `
				[
					{
						"kid": "identity-current",
						"public_key_path": "/current-1.pub"
					},
					{
						"kid": " identity-current ",
						"public_key_path": "/current-2.pub"
					}
				]
			`,
			activeKeyID:   "identity-current",
			expectedError: `duplicate access token verification key ID "identity-current"`,
		},
		{
			name: "active key missing",
			rawKeyring: `
				[
					{
						"kid": "identity-previous",
						"public_key_path": "/previous.pub"
					}
				]
			`,
			activeKeyID:   "identity-current",
			expectedError: `active access token key ID "identity-current" is not present`,
		},
		{
			name: "trailing JSON",
			rawKeyring: `
				[
					{
						"kid": "identity-current",
						"public_key_path": "/current.pub"
					}
				]
				{}
			`,
			activeKeyID:   "identity-current",
			expectedError: "contains trailing JSON data",
		},
	}

	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				_, err := parseAccessTokenVerificationKeys(
					test.rawKeyring,
					test.legacyPublicKeyPath,
					test.activeKeyID,
				)

				if err == nil {
					t.Fatal(
						"parseAccessTokenVerificationKeys() returned nil error",
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

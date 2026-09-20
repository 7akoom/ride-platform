package config

import (
	"strings"
	"testing"
)

func TestValidateSecrets(t *testing.T) {
	cases := []struct {
		name        string
		environment string
		token       string
		wantErr     bool
	}{
		{"development runs with the placeholder", "development", developmentInternalServiceToken, false},
		{"development runs with a short token", "development", "abc", false},
		{"production refuses the placeholder", "production", developmentInternalServiceToken, true},
		{"staging refuses the placeholder", "staging", developmentInternalServiceToken, true},
		{"an unset environment is not development", "", developmentInternalServiceToken, true},
		{"production refuses a token one character short", "production", strings.Repeat("a", minInternalServiceTokenLength-1), true},
		{"production accepts a token of exactly the minimum length", "production", strings.Repeat("a", minInternalServiceTokenLength), false},
		{"production accepts a 64 character secret", "production", strings.Repeat("f", 64), false},
		{"an empty token is refused in development", "development", "", true},
		{"an empty token is refused in production", "production", "", true},
	}

	for _, tc := range cases {
		err := ValidateSecrets(Config{Environment: tc.environment, InternalServiceToken: tc.token})

		if tc.wantErr && err == nil {
			t.Errorf("%s: expected an error, got none", tc.name)
		}

		if !tc.wantErr && err != nil {
			t.Errorf("%s: unexpected error: %v", tc.name, err)
		}
	}
}

func TestValidateSecretsNeverEchoesTheToken(t *testing.T) {
	const short = "s3cr3t-but-short"

	err := ValidateSecrets(Config{Environment: "production", InternalServiceToken: short})
	if err == nil {
		t.Fatal("expected an error for a short token")
	}

	if strings.Contains(err.Error(), short) {
		t.Errorf("the error leaks the token: %v", err)
	}
}

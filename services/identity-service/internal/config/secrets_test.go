package config

import (
	"strings"
	"testing"
)

func TestTheInternalServiceTokenIsGuarded(t *testing.T) {
	strong := strings.Repeat("a1", 32)

	cases := []struct {
		name        string
		environment string
		token       string
		ok          bool
	}{
		{"development with the placeholder", "development", developmentInternalServiceToken, true},
		{"production with the placeholder", "production", developmentInternalServiceToken, false},
		{"no environment with the placeholder", "", developmentInternalServiceToken, false},
		{"production with a short token", "production", "short", false},
		{"production with a real secret", "production", strong, true},
		{"an empty token", "development", " ", false},
	}

	for _, tc := range cases {
		err := ValidateSecrets(Config{Environment: tc.environment, InternalServiceToken: tc.token})
		if (err == nil) != tc.ok {
			t.Fatalf("%s: %v", tc.name, err)
		}
	}
}

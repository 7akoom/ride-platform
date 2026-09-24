package config

import (
	"errors"
	"fmt"
	"strings"
)

// developmentInternalServiceToken is the placeholder checked into the repo and
// into every .env.example. It exists so a fresh checkout runs locally; anyone
// who has read the code knows it.
const developmentInternalServiceToken = "dev-internal-service-token-change-me"

// minInternalServiceTokenLength is the shortest secret accepted outside
// development. `openssl rand -hex 32` produces 64 characters.
const minInternalServiceTokenLength = 32

// ValidateSecrets refuses an internal service token that would let anyone
// who has read the repository verify wallet PINs (and so guess them against
// the lock of someone else) or look people up by phone. Only
// APP_ENV=development may run with the placeholder.
func ValidateSecrets(cfg Config) error {
	token := strings.TrimSpace(cfg.InternalServiceToken)

	if token == "" {
		return errors.New("INTERNAL_SERVICE_TOKEN is empty")
	}

	environment := strings.ToLower(strings.TrimSpace(cfg.Environment))
	if environment == "development" {
		return nil
	}

	if token == developmentInternalServiceToken {
		return fmt.Errorf(
			"INTERNAL_SERVICE_TOKEN is still the development placeholder while APP_ENV is %q; "+
				"generate a secret (for example `openssl rand -hex 32`) and set the same value on every service",
			cfg.Environment,
		)
	}

	if len(token) < minInternalServiceTokenLength {
		return fmt.Errorf(
			"INTERNAL_SERVICE_TOKEN is shorter than %d characters while APP_ENV is %q; "+
				"generate a secret (for example `openssl rand -hex 32`)",
			minInternalServiceTokenLength,
			cfg.Environment,
		)
	}

	return nil
}

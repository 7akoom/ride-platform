package config

import (
	"errors"
	"fmt"
)

// developmentInternalServiceToken is the placeholder checked into the repo and
// into every .env.example. It exists so a fresh checkout runs locally; anyone
// who has read the code knows it.
const developmentInternalServiceToken = "dev-internal-service-token-change-me"

// minInternalServiceTokenLength is the shortest secret accepted outside
// development. `openssl rand -hex 32` produces 64 characters.
const minInternalServiceTokenLength = 32

// ValidateSecrets refuses configurations that would leave the service-to-service
// path open. The internal service token bypasses every ownership check, so a
// deployment that still uses the published placeholder (or a short guessable
// value) lets anyone who has read the repository act as any service.
//
// Only ENVIRONMENT=development is allowed to run with the placeholder.
// Anything else, including an unset value in a hand-built Config, must carry
// a real secret.
func ValidateSecrets(cfg Config) error {
	token := cfg.InternalServiceToken

	if token == "" {
		return errors.New("INTERNAL_SERVICE_TOKEN is empty")
	}

	if cfg.Environment == "development" {
		return nil
	}

	if token == developmentInternalServiceToken {
		return fmt.Errorf(
			"INTERNAL_SERVICE_TOKEN is still the development placeholder while ENVIRONMENT is %q; "+
				"generate a secret (for example `openssl rand -hex 32`) and set the same value on every service",
			cfg.Environment,
		)
	}

	if len(token) < minInternalServiceTokenLength {
		return fmt.Errorf(
			"INTERNAL_SERVICE_TOKEN is shorter than %d characters while ENVIRONMENT is %q; "+
				"generate a secret (for example `openssl rand -hex 32`)",
			minInternalServiceTokenLength,
			cfg.Environment,
		)
	}

	return nil
}

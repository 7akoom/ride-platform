package config

import (
	"os"
	"testing"
)

func TestLoadDoesNotDefaultEnvironmentToDevelopment(
	t *testing.T,
) {
	originalValue, existed := os.LookupEnv(
		"APP_ENV",
	)

	if err := os.Unsetenv("APP_ENV"); err != nil {
		t.Fatalf(
			"unset APP_ENV: %v",
			err,
		)
	}

	t.Cleanup(func() {
		if existed {
			if err := os.Setenv(
				"APP_ENV",
				originalValue,
			); err != nil {
				t.Errorf(
					"restore APP_ENV: %v",
					err,
				)
			}

			return
		}

		if err := os.Unsetenv("APP_ENV"); err != nil {
			t.Errorf(
				"clean APP_ENV: %v",
				err,
			)
		}
	})

	cfg := Load()

	if cfg.Environment != "" {
		t.Fatalf(
			"Environment = %q, expected empty value when APP_ENV is missing",
			cfg.Environment,
		)
	}
}

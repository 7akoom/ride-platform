//go:build integration

package database

import (
	"context"
	"os"
	"testing"
)

func TestNewValkeyClientConnectsToValkey(
	t *testing.T,
) {
	valkeyAddress := os.Getenv("VALKEY_ADDRESS")
	if valkeyAddress == "" {
		t.Fatal(
			"VALKEY_ADDRESS is required for integration test",
		)
	}

	valkeyPassword := os.Getenv("VALKEY_PASSWORD")
	if valkeyPassword == "" {
		t.Fatal(
			"VALKEY_PASSWORD is required for integration test",
		)
	}

	ctx := context.Background()

	client, err := NewValkeyClient(
		ctx,
		valkeyAddress,
		valkeyPassword,
	)
	if err != nil {
		t.Fatalf(
			"NewValkeyClient() returned an error: %v",
			err,
		)
	}
	defer client.Close()

	pong, err := client.Do(
		ctx,
		client.B().
			Ping().
			Build(),
	).ToString()
	if err != nil {
		t.Fatalf(
			"PING returned an error: %v",
			err,
		)
	}

	if pong != "PONG" {
		t.Fatalf(
			"PING returned %q, expected %q",
			pong,
			"PONG",
		)
	}

	t.Log(
		"connected to Valkey successfully",
	)
}

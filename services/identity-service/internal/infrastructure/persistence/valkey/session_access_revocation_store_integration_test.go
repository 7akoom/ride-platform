//go:build integration

package valkey

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/database"
)

func TestSessionAccessRevocationStoreMarksSessionRevokedWithTTL(
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

	client, err := database.NewValkeyClient(
		ctx,
		valkeyAddress,
		valkeyPassword,
	)
	if err != nil {
		t.Fatalf(
			"connect to Valkey: %v",
			err,
		)
	}
	defer client.Close()

	store := NewSessionAccessRevocationStore(
		client,
	)

	sessionID := fmt.Sprintf(
		"integration-session-%d",
		time.Now().UnixNano(),
	)

	revoked, err := store.IsRevoked(
		ctx,
		sessionID,
	)
	if err != nil {
		t.Fatalf(
			"IsRevoked() before marking returned an error: %v",
			err,
		)
	}

	if revoked {
		t.Fatal(
			"IsRevoked() returned true before session was marked revoked",
		)
	}

	const revocationTTL = 30 * time.Second

	err = store.MarkRevoked(
		ctx,
		sessionID,
		revocationTTL,
	)
	if err != nil {
		t.Fatalf(
			"MarkRevoked() returned an error: %v",
			err,
		)
	}

	revoked, err = store.IsRevoked(
		ctx,
		sessionID,
	)
	if err != nil {
		t.Fatalf(
			"IsRevoked() after marking returned an error: %v",
			err,
		)
	}

	if !revoked {
		t.Fatal(
			"IsRevoked() returned false after session was marked revoked",
		)
	}

	key := revokedSessionKeyPrefix + sessionID

	value, err := client.Do(
		ctx,
		client.B().
			Get().
			Key(key).
			Build(),
	).ToString()
	if err != nil {
		t.Fatalf(
			"read revocation marker: %v",
			err,
		)
	}

	if value != "1" {
		t.Fatalf(
			"revocation marker value = %q, expected %q",
			value,
			"1",
		)
	}

	ttlSeconds, err := client.Do(
		ctx,
		client.B().
			Ttl().
			Key(key).
			Build(),
	).ToInt64()
	if err != nil {
		t.Fatalf(
			"read revocation marker TTL: %v",
			err,
		)
	}

	if ttlSeconds <= 0 {
		t.Fatalf(
			"revocation marker TTL = %d, expected positive TTL",
			ttlSeconds,
		)
	}

	if ttlSeconds > int64(revocationTTL.Seconds()) {
		t.Fatalf(
			"revocation marker TTL = %d, expected at most %d",
			ttlSeconds,
			int64(revocationTTL.Seconds()),
		)
	}
}

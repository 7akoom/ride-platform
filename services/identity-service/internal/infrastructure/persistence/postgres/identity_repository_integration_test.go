//go:build integration

package postgres

import (
	"context"
	"os"
	"testing"

	databaseinfra "github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/database"
)

func TestIdentityRepositoryFindOrCreateReturnsSameIdentityForSamePhoneNumber(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("DATABASE_URL is required for integration test")
	}

	ctx := context.Background()

	pool, err := databaseinfra.NewPostgresPool(
		ctx,
		databaseURL,
	)
	if err != nil {
		t.Fatalf("connect to PostgreSQL: %v", err)
	}

	t.Cleanup(pool.Close)

	repository := NewIdentityRepository(pool)

	const phoneNumber = "+9647500000002"

	_, err = pool.Exec(
		ctx,
		"DELETE FROM identities WHERE phone_number = $1",
		phoneNumber,
	)
	if err != nil {
		t.Fatalf("clean existing test identity: %v", err)
	}

	t.Cleanup(func() {
		_, cleanupErr := pool.Exec(
			context.Background(),
			"DELETE FROM identities WHERE phone_number = $1",
			phoneNumber,
		)
		if cleanupErr != nil {
			t.Errorf("clean test identity: %v", cleanupErr)
		}
	})

	firstIdentity, err := repository.FindOrCreateByPhoneNumber(
		ctx,
		phoneNumber,
	)
	if err != nil {
		t.Fatalf(
			"first FindOrCreateByPhoneNumber() returned an error: %v",
			err,
		)
	}

	secondIdentity, err := repository.FindOrCreateByPhoneNumber(
		ctx,
		phoneNumber,
	)
	if err != nil {
		t.Fatalf(
			"second FindOrCreateByPhoneNumber() returned an error: %v",
			err,
		)
	}

	if firstIdentity.ID == "" {
		t.Fatal("first identity has an empty ID")
	}

	if secondIdentity.ID == "" {
		t.Fatal("second identity has an empty ID")
	}

	if firstIdentity.ID != secondIdentity.ID {
		t.Fatalf(
			"same phone number returned different identity IDs: first=%q second=%q",
			firstIdentity.ID,
			secondIdentity.ID,
		)
	}

	if firstIdentity.PhoneNumber != phoneNumber {
		t.Fatalf(
			"first identity phone number is %q, expected %q",
			firstIdentity.PhoneNumber,
			phoneNumber,
		)
	}

	if secondIdentity.PhoneNumber != phoneNumber {
		t.Fatalf(
			"second identity phone number is %q, expected %q",
			secondIdentity.PhoneNumber,
			phoneNumber,
		)
	}

	if !firstIdentity.IsActive {
		t.Fatal("new identity is not active")
	}

	var count int

	if err := pool.QueryRow(
		ctx,
		"SELECT COUNT(*) FROM identities WHERE phone_number = $1",
		phoneNumber,
	).Scan(&count); err != nil {
		t.Fatalf("count test identities: %v", err)
	}

	if count != 1 {
		t.Fatalf(
			"database contains %d identities for the same phone number, expected 1",
			count,
		)
	}
}
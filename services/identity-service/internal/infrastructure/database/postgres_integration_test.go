//go:build integration

package database

import (
	"context"
	"os"
	"testing"
)

func TestNewPostgresPoolConnectsToDatabase(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("DATABASE_URL is required for integration test")
	}

	ctx := context.Background()

	pool, err := NewPostgresPool(ctx, databaseURL)
	if err != nil {
		t.Fatalf("NewPostgresPool() returned an error: %v", err)
	}
	defer pool.Close()

	var databaseName string
	var databaseUser string

	err = pool.QueryRow(
		ctx,
		"SELECT current_database(), current_user",
	).Scan(
		&databaseName,
		&databaseUser,
	)
	if err != nil {
		t.Fatalf("query PostgreSQL connection information: %v", err)
	}

	if databaseName != "identity_db" {
		t.Fatalf(
			"connected to database %q, expected %q",
			databaseName,
			"identity_db",
		)
	}

	if databaseUser != "identity_user" {
		t.Fatalf(
			"connected as user %q, expected %q",
			databaseUser,
			"identity_user",
		)
	}

	t.Logf(
		"connected to PostgreSQL database=%s user=%s",
		databaseName,
		databaseUser,
	)
}
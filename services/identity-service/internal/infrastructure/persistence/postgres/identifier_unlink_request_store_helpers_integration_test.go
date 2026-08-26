//go:build integration

package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	databaseinfra "github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/database"
	"github.com/jackc/pgx/v5/pgxpool"
)

func newIdentifierUnlinkRequestIntegrationTest(
	t *testing.T,
) (
	context.Context,
	*pgxpool.Pool,
	*IdentifierUnlinkRequestStore,
) {
	t.Helper()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Fatal(
			"DATABASE_URL is required for integration test",
		)
	}

	ctx := context.Background()

	pool, err := databaseinfra.NewPostgresPool(
		ctx,
		databaseURL,
	)
	if err != nil {
		t.Fatalf(
			"connect to PostgreSQL: %v",
			err,
		)
	}

	t.Cleanup(pool.Close)

	return ctx,
		pool,
		NewIdentifierUnlinkRequestStore(pool)
}

func createIdentifierUnlinkTestIdentity(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) string {
	t.Helper()

	var identityID string

	err := pool.QueryRow(
		ctx,
		`
			INSERT INTO identities
				DEFAULT VALUES
			RETURNING id::text
		`,
	).Scan(
		&identityID,
	)
	if err != nil {
		t.Fatalf(
			"create identifier unlink test identity: %v",
			err,
		)
	}

	t.Cleanup(func() {
		if _, cleanupErr := pool.Exec(
			context.Background(),
			`
				DELETE FROM identities
				WHERE id = $1::uuid
			`,
			identityID,
		); cleanupErr != nil {
			t.Errorf(
				"clean identifier unlink test identity %q: %v",
				identityID,
				cleanupErr,
			)
		}
	})

	return identityID
}

func insertIdentifierUnlinkTestIdentifier(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	identityID string,
	identifier auth.Identifier,
) {
	t.Helper()

	normalizedIdentifier, err := auth.NewIdentifier(
		identifier.Type,
		identifier.Value,
	)
	if err != nil {
		t.Fatalf(
			"normalize identifier unlink test identifier: %v",
			err,
		)
	}

	if _, err := pool.Exec(
		ctx,
		`
			INSERT INTO identity_identifiers (
				identity_id,
				identifier_type,
				normalized_value,
				verified_at
			)
			VALUES ($1::uuid, $2, $3, $4)
		`,
		identityID,
		string(normalizedIdentifier.Type),
		normalizedIdentifier.Value,
		time.Now().UTC(),
	); err != nil {
		t.Fatalf(
			"insert identifier unlink test identifier: %v",
			err,
		)
	}
}

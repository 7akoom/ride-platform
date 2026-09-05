//go:build integration

package postgres

import (
	"context"
	"os"
	"testing"

	databaseinfra "github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/database"
	"github.com/jackc/pgx/v5/pgxpool"
)

func newIdentifierLinkCompletionIntegrationTest(
	t *testing.T,
) (
	context.Context,
	*pgxpool.Pool,
	*IdentifierLinkCompletionStore,
	*ChallengeRepository,
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
		NewIdentifierLinkCompletionStore(pool),
		NewChallengeRepository(pool)
}

func createIdentifierLinkTestIdentity(
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
			"create identifier link test identity: %v",
			err,
		)
	}

	t.Cleanup(func() {
		if _, cleanupErr := pool.Exec(
			context.Background(),
			`
				DELETE FROM identities
				WHERE id = $1
			`,
			identityID,
		); cleanupErr != nil {
			t.Errorf(
				"clean identifier link test identity %q: %v",
				identityID,
				cleanupErr,
			)
		}
	})

	return identityID
}

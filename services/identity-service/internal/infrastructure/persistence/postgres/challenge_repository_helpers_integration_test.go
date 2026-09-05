//go:build integration

package postgres

import (
	"context"
	"os"
	"testing"

	databaseinfra "github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/database"
	"github.com/jackc/pgx/v5/pgxpool"
)

func newChallengeRepositoryIntegrationTest(
	t *testing.T,
) (
	context.Context,
	*pgxpool.Pool,
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

	return ctx, pool, NewChallengeRepository(pool)
}

func cleanupChallengeIDs(
	t *testing.T,
	pool *pgxpool.Pool,
	challengeIDs ...string,
) {
	t.Helper()

	cleanup := func() {
		for _, challengeID := range challengeIDs {
			if _, err := pool.Exec(
				context.Background(),
				`
					DELETE FROM otp_challenges
					WHERE id = $1
				`,
				challengeID,
			); err != nil {
				t.Errorf(
					"clean test challenge %q: %v",
					challengeID,
					err,
				)
			}
		}
	}

	cleanup()

	t.Cleanup(cleanup)
}

func createIntegrationIdentity(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) string {
	t.Helper()

	var identityID string

	if err := pool.QueryRow(
		ctx,
		`
			INSERT INTO identities
				DEFAULT VALUES
			RETURNING id::text
		`,
	).Scan(
		&identityID,
	); err != nil {
		t.Fatalf(
			"create integration test identity: %v",
			err,
		)
	}

	t.Cleanup(func() {
		if _, err := pool.Exec(
			context.Background(),
			`
				DELETE FROM identities
				WHERE id = $1
			`,
			identityID,
		); err != nil {
			t.Errorf(
				"clean integration test identity %q: %v",
				identityID,
				err,
			)
		}
	})

	return identityID
}

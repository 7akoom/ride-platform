//go:build integration

package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	databaseinfra "github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/database"
	"github.com/jackc/pgx/v5/pgxpool"
)

type identityIdentifierRepositoryIntegrationFixture struct {
	t          *testing.T
	ctx        context.Context
	pool       *pgxpool.Pool
	repository *IdentityIdentifierRepository
	verifiedAt time.Time
}

func newIdentityIdentifierRepositoryIntegrationFixture(
	t *testing.T,
) *identityIdentifierRepositoryIntegrationFixture {
	t.Helper()

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
		t.Fatalf(
			"connect to PostgreSQL: %v",
			err,
		)
	}

	t.Cleanup(pool.Close)

	return &identityIdentifierRepositoryIntegrationFixture{
		t:          t,
		ctx:        ctx,
		pool:       pool,
		repository: NewIdentityIdentifierRepository(pool),
		verifiedAt: time.Now().UTC(),
	}
}

func (f *identityIdentifierRepositoryIntegrationFixture) prepareCleanup(
	values ...string,
) {
	f.t.Helper()

	f.cleanup(values...)

	f.t.Cleanup(func() {
		f.cleanup(values...)
	})
}

func (f *identityIdentifierRepositoryIntegrationFixture) cleanup(
	values ...string,
) {
	f.t.Helper()

	const query = `
		DELETE FROM identities
		WHERE id IN (
			SELECT identity_id
			FROM identity_identifiers
			WHERE normalized_value = $1
		)
	`

	for _, value := range values {
		if _, err := f.pool.Exec(
			context.Background(),
			query,
			value,
		); err != nil {
			f.t.Fatalf(
				"clean integration test identity for %q: %v",
				value,
				err,
			)
		}
	}
}

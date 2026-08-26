//go:build integration

package postgres

import (
	"sync"
	"testing"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

func TestIdentityIdentifierRepositoryConcurrentCreateReturnsOneIdentity(
	t *testing.T,
) {
	fixture :=
		newIdentityIdentifierRepositoryIntegrationFixture(t)

	const emailAddress = "concurrent@example.com"
	const workers = 8

	fixture.prepareCleanup(
		emailAddress,
	)

	type result struct {
		identity auth.Identity
		err      error
	}

	start := make(chan struct{})
	results := make(chan result, workers)

	var waitGroup sync.WaitGroup

	for range workers {
		waitGroup.Add(1)

		go func() {
			defer waitGroup.Done()

			<-start

			identity, err :=
				fixture.repository.CreateIdentityWithIdentifier(
					fixture.ctx,
					auth.Identifier{
						Type:  auth.IdentifierTypeEmail,
						Value: emailAddress,
					},
					fixture.verifiedAt,
				)

			results <- result{
				identity: identity,
				err:      err,
			}
		}()
	}

	close(start)

	waitGroup.Wait()
	close(results)

	var identityID string

	for result := range results {
		if result.err != nil {
			t.Fatalf(
				"concurrent CreateIdentityWithIdentifier() returned an error: %v",
				result.err,
			)
		}

		if result.identity.ID == "" {
			t.Fatal(
				"concurrent CreateIdentityWithIdentifier() returned empty identity ID",
			)
		}

		if identityID == "" {
			identityID = result.identity.ID
			continue
		}

		if result.identity.ID != identityID {
			t.Fatalf(
				"concurrent creation returned different identity IDs: got=%q want=%q",
				result.identity.ID,
				identityID,
			)
		}
	}

	var identifierCount int
	var identityCount int

	if err := fixture.pool.QueryRow(
		fixture.ctx,
		`
			SELECT
				COUNT(*),
				COUNT(DISTINCT identity_id)
			FROM identity_identifiers
			WHERE identifier_type = 'email'
			  AND normalized_value = $1
		`,
		emailAddress,
	).Scan(
		&identifierCount,
		&identityCount,
	); err != nil {
		t.Fatalf(
			"count concurrent identifier records: %v",
			err,
		)
	}

	if identifierCount != 1 {
		t.Fatalf(
			"concurrent identifier count = %d, want 1",
			identifierCount,
		)
	}

	if identityCount != 1 {
		t.Fatalf(
			"concurrent identity count = %d, want 1",
			identityCount,
		)
	}
}

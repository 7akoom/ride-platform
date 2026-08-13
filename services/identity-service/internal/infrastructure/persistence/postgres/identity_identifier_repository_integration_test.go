//go:build integration

package postgres

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	databaseinfra "github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/database"
)

func TestIdentityIdentifierRepositoryIntegration(t *testing.T) {
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

	repository := NewIdentityIdentifierRepository(pool)

	cleanup := func(
		t *testing.T,
		values ...string,
	) {
		t.Helper()

		const query = `
			DELETE FROM identities
			WHERE id IN (
				SELECT identity_id
				FROM identity_identifiers
				WHERE normalized_value = $1
			)
		`

		for _, value := range values {
			if _, err := pool.Exec(
				context.Background(),
				query,
				value,
			); err != nil {
				t.Fatalf(
					"clean integration test identity for %q: %v",
					value,
					err,
				)
			}
		}
	}

	prepareCleanup := func(
		t *testing.T,
		values ...string,
	) {
		t.Helper()

		cleanup(t, values...)

		t.Cleanup(func() {
			cleanup(t, values...)
		})
	}

	verifiedAt := time.Now().UTC()

	t.Run("creates and finds identity by phone", func(t *testing.T) {
		const phoneNumber = "+9647500000101"

		prepareCleanup(
			t,
			phoneNumber,
		)

		identity, err := repository.CreateIdentityWithIdentifier(
			ctx,
			auth.Identifier{
				Type:  auth.IdentifierTypePhone,
				Value: phoneNumber,
			},
			verifiedAt,
		)
		if err != nil {
			t.Fatalf(
				"CreateIdentityWithIdentifier() returned an error: %v",
				err,
			)
		}

		if identity.ID == "" {
			t.Fatal("created identity has an empty ID")
		}

		if !identity.IsActive {
			t.Fatal("new identity is not active")
		}

		foundIdentity, found, err := repository.FindIdentityByIdentifier(
			ctx,
			auth.Identifier{
				Type:  auth.IdentifierTypePhone,
				Value: phoneNumber,
			},
		)
		if err != nil {
			t.Fatalf(
				"FindIdentityByIdentifier() returned an error: %v",
				err,
			)
		}

		if !found {
			t.Fatal("created phone identity was not found")
		}

		if foundIdentity.ID != identity.ID {
			t.Fatalf(
				"found identity ID = %q, want %q",
				foundIdentity.ID,
				identity.ID,
			)
		}
	})

	t.Run("creates email only identity", func(t *testing.T) {
		const inputEmail = "EmailOnly@EXAMPLE.COM"
		const canonicalEmail = "emailonly@example.com"

		prepareCleanup(
			t,
			canonicalEmail,
		)

		identity, err := repository.CreateIdentityWithIdentifier(
			ctx,
			auth.Identifier{
				Type:  auth.IdentifierTypeEmail,
				Value: inputEmail,
			},
			verifiedAt,
		)
		if err != nil {
			t.Fatalf(
				"CreateIdentityWithIdentifier() returned an error: %v",
				err,
			)
		}

		if identity.ID == "" {
			t.Fatal("created identity has an empty ID")
		}

		if !identity.IsActive {
			t.Fatal("new email identity is not active")
		}

		foundIdentity, found, err := repository.FindIdentityByIdentifier(
			ctx,
			auth.Identifier{
				Type:  auth.IdentifierTypeEmail,
				Value: "EMAILONLY@example.com",
			},
		)
		if err != nil {
			t.Fatalf(
				"FindIdentityByIdentifier() returned an error: %v",
				err,
			)
		}

		if !found {
			t.Fatal("created email identity was not found")
		}

		if foundIdentity.ID != identity.ID {
			t.Fatalf(
				"found identity ID = %q, want %q",
				foundIdentity.ID,
				identity.ID,
			)
		}
	})

	t.Run("create is idempotent for same identifier", func(t *testing.T) {
		const emailAddress = "idempotent@example.com"

		prepareCleanup(
			t,
			emailAddress,
		)

		firstIdentity, err := repository.CreateIdentityWithIdentifier(
			ctx,
			auth.Identifier{
				Type:  auth.IdentifierTypeEmail,
				Value: emailAddress,
			},
			verifiedAt,
		)
		if err != nil {
			t.Fatalf(
				"first CreateIdentityWithIdentifier() returned an error: %v",
				err,
			)
		}

		secondIdentity, err := repository.CreateIdentityWithIdentifier(
			ctx,
			auth.Identifier{
				Type:  auth.IdentifierTypeEmail,
				Value: emailAddress,
			},
			verifiedAt,
		)
		if err != nil {
			t.Fatalf(
				"second CreateIdentityWithIdentifier() returned an error: %v",
				err,
			)
		}

		if firstIdentity.ID != secondIdentity.ID {
			t.Fatalf(
				"same identifier returned different identities: first=%q second=%q",
				firstIdentity.ID,
				secondIdentity.ID,
			)
		}

		var count int

		if err := pool.QueryRow(
			ctx,
			`
				SELECT COUNT(*)
				FROM identity_identifiers
				WHERE identifier_type = 'email'
				AND normalized_value = $1
			`,
			emailAddress,
		).Scan(
			&count,
		); err != nil {
			t.Fatalf(
				"count idempotent identifier records: %v",
				err,
			)
		}

		if count != 1 {
			t.Fatalf(
				"identifier record count = %d, want 1",
				count,
			)
		}
	})

	t.Run("links email to existing phone identity", func(t *testing.T) {
		const phoneNumber = "+9647500000102"
		const emailAddress = "linked-email@example.com"

		prepareCleanup(
			t,
			phoneNumber,
			emailAddress,
		)

		identity, err := repository.CreateIdentityWithIdentifier(
			ctx,
			auth.Identifier{
				Type:  auth.IdentifierTypePhone,
				Value: phoneNumber,
			},
			verifiedAt,
		)
		if err != nil {
			t.Fatalf(
				"create phone identity: %v",
				err,
			)
		}

		err = repository.LinkIdentifier(
			ctx,
			identity.ID,
			auth.Identifier{
				Type:  auth.IdentifierTypeEmail,
				Value: emailAddress,
			},
			verifiedAt,
		)
		if err != nil {
			t.Fatalf(
				"LinkIdentifier() returned an error: %v",
				err,
			)
		}

		err = repository.LinkIdentifier(
			ctx,
			identity.ID,
			auth.Identifier{
				Type:  auth.IdentifierTypeEmail,
				Value: emailAddress,
			},
			verifiedAt,
		)
		if err != nil {
			t.Fatalf(
				"idempotent LinkIdentifier() returned an error: %v",
				err,
			)
		}

		foundIdentity, found, err := repository.FindIdentityByIdentifier(
			ctx,
			auth.Identifier{
				Type:  auth.IdentifierTypeEmail,
				Value: emailAddress,
			},
		)
		if err != nil {
			t.Fatalf(
				"find identity by linked email: %v",
				err,
			)
		}

		if !found {
			t.Fatal("identity was not found by linked email")
		}

		if foundIdentity.ID != identity.ID {
			t.Fatalf(
				"linked email belongs to identity %q, want %q",
				foundIdentity.ID,
				identity.ID,
			)
		}
	})

	t.Run("links phone to email only identity", func(t *testing.T) {
		const emailAddress = "email-to-phone@example.com"
		const phoneNumber = "+9647500000103"

		prepareCleanup(
			t,
			emailAddress,
			phoneNumber,
		)

		identity, err := repository.CreateIdentityWithIdentifier(
			ctx,
			auth.Identifier{
				Type:  auth.IdentifierTypeEmail,
				Value: emailAddress,
			},
			verifiedAt,
		)
		if err != nil {
			t.Fatalf(
				"create email identity: %v",
				err,
			)
		}

		err = repository.LinkIdentifier(
			ctx,
			identity.ID,
			auth.Identifier{
				Type:  auth.IdentifierTypePhone,
				Value: phoneNumber,
			},
			verifiedAt,
		)
		if err != nil {
			t.Fatalf(
				"link phone identifier: %v",
				err,
			)
		}

		foundIdentity, found, err := repository.FindIdentityByIdentifier(
			ctx,
			auth.Identifier{
				Type:  auth.IdentifierTypePhone,
				Value: phoneNumber,
			},
		)
		if err != nil {
			t.Fatalf(
				"find identity by linked phone: %v",
				err,
			)
		}

		if !found {
			t.Fatal("identity was not found by linked phone")
		}

		if foundIdentity.ID != identity.ID {
			t.Fatalf(
				"linked phone belongs to identity %q, want %q",
				foundIdentity.ID,
				identity.ID,
			)
		}
	})

	t.Run("rejects identifier owned by another identity", func(t *testing.T) {
		const firstPhone = "+9647500000104"
		const secondPhone = "+9647500000105"
		const emailAddress = "ownership@example.com"

		prepareCleanup(
			t,
			firstPhone,
			secondPhone,
			emailAddress,
		)

		firstIdentity, err := repository.CreateIdentityWithIdentifier(
			ctx,
			auth.Identifier{
				Type:  auth.IdentifierTypePhone,
				Value: firstPhone,
			},
			verifiedAt,
		)
		if err != nil {
			t.Fatalf(
				"create first identity: %v",
				err,
			)
		}

		secondIdentity, err := repository.CreateIdentityWithIdentifier(
			ctx,
			auth.Identifier{
				Type:  auth.IdentifierTypePhone,
				Value: secondPhone,
			},
			verifiedAt,
		)
		if err != nil {
			t.Fatalf(
				"create second identity: %v",
				err,
			)
		}

		if err := repository.LinkIdentifier(
			ctx,
			firstIdentity.ID,
			auth.Identifier{
				Type:  auth.IdentifierTypeEmail,
				Value: emailAddress,
			},
			verifiedAt,
		); err != nil {
			t.Fatalf(
				"link email to first identity: %v",
				err,
			)
		}

		err = repository.LinkIdentifier(
			ctx,
			secondIdentity.ID,
			auth.Identifier{
				Type:  auth.IdentifierTypeEmail,
				Value: emailAddress,
			},
			verifiedAt,
		)

		if !errors.Is(
			err,
			auth.ErrIdentifierAlreadyLinked,
		) {
			t.Fatalf(
				"LinkIdentifier() error = %v, want %v",
				err,
				auth.ErrIdentifierAlreadyLinked,
			)
		}
	})

	t.Run("concurrent create returns one identity", func(t *testing.T) {
		const emailAddress = "concurrent@example.com"
		const workers = 8

		prepareCleanup(
			t,
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

				identity, err := repository.CreateIdentityWithIdentifier(
					ctx,
					auth.Identifier{
						Type:  auth.IdentifierTypeEmail,
						Value: emailAddress,
					},
					verifiedAt,
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

		if err := pool.QueryRow(
			ctx,
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
	})
}

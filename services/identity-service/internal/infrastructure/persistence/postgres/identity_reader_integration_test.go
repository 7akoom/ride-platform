//go:build integration

package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	databaseinfra "github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/database"
)

func TestIdentityReaderIntegration(t *testing.T) {
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

	reader := NewIdentityReader(pool)

	createIdentity := func(
		t *testing.T,
		status auth.IdentityStatus,
	) string {
		t.Helper()

		const query = `
			INSERT INTO identities (
				status
			)
			VALUES ($1)
			RETURNING id::text
		`

		var identityID string

		if err := pool.QueryRow(
			ctx,
			query,
			string(status),
		).Scan(&identityID); err != nil {
			t.Fatalf(
				"create integration test identity: %v",
				err,
			)
		}

		t.Cleanup(func() {
			if _, err := pool.Exec(
				context.Background(),
				`DELETE FROM identities WHERE id = $1::uuid`,
				identityID,
			); err != nil {
				t.Fatalf(
					"clean integration test identity %q: %v",
					identityID,
					err,
				)
			}
		})

		return identityID
	}

	insertIdentifier := func(
		t *testing.T,
		identityID string,
		identifierType auth.IdentifierType,
		value string,
		verifiedAt time.Time,
		createdAt time.Time,
	) {
		t.Helper()

		const query = `
			INSERT INTO identity_identifiers (
				identity_id,
				identifier_type,
				normalized_value,
				verified_at,
				created_at,
				updated_at
			)
			VALUES (
				$1::uuid,
				$2,
				$3,
				$4,
				$5,
				$5
			)
		`

		if _, err := pool.Exec(
			ctx,
			query,
			identityID,
			string(identifierType),
			value,
			verifiedAt.UTC(),
			createdAt.UTC(),
		); err != nil {
			t.Fatalf(
				"insert integration test identifier: %v",
				err,
			)
		}
	}

	t.Run("returns identity with ordered identifiers", func(t *testing.T) {
		identityID := createIdentity(
			t,
			auth.IdentityStatusActive,
		)

		baseTime := time.Now().UTC().Add(-time.Hour)

		insertIdentifier(
			t,
			identityID,
			auth.IdentifierTypePhone,
			"+9647500000201",
			baseTime,
			baseTime,
		)

		insertIdentifier(
			t,
			identityID,
			auth.IdentifierTypeEmail,
			"reader-integration@example.com",
			baseTime.Add(time.Minute),
			baseTime.Add(time.Minute),
		)

		details, found, err := reader.FindByID(
			ctx,
			identityID,
		)
		if err != nil {
			t.Fatalf(
				"FindByID() returned an error: %v",
				err,
			)
		}

		if !found {
			t.Fatal("identity was not found")
		}

		if details.ID != identityID {
			t.Fatalf(
				"identity ID = %q, want %q",
				details.ID,
				identityID,
			)
		}

		if details.Status != auth.IdentityStatusActive {
			t.Fatalf(
				"identity status = %q, want %q",
				details.Status,
				auth.IdentityStatusActive,
			)
		}

		if len(details.Identifiers) != 2 {
			t.Fatalf(
				"identifiers count = %d, want 2",
				len(details.Identifiers),
			)
		}

		if details.Identifiers[0].Identifier.Type != auth.IdentifierTypePhone {
			t.Fatalf(
				"first identifier type = %q, want %q",
				details.Identifiers[0].Identifier.Type,
				auth.IdentifierTypePhone,
			)
		}

		if details.Identifiers[0].Identifier.Value != "+9647500000201" {
			t.Fatalf(
				"first identifier value = %q",
				details.Identifiers[0].Identifier.Value,
			)
		}

		if details.Identifiers[1].Identifier.Type != auth.IdentifierTypeEmail {
			t.Fatalf(
				"second identifier type = %q, want %q",
				details.Identifiers[1].Identifier.Type,
				auth.IdentifierTypeEmail,
			)
		}

		if details.Identifiers[1].Identifier.Value != "reader-integration@example.com" {
			t.Fatalf(
				"second identifier value = %q",
				details.Identifiers[1].Identifier.Value,
			)
		}

		if !details.Identifiers[0].VerifiedAt.Equal(
			baseTime.Truncate(time.Microsecond),
		) {
			t.Fatalf(
				"first identifier verified_at = %v, want %v",
				details.Identifiers[0].VerifiedAt,
				baseTime.Truncate(time.Microsecond),
			)
		}

		if !details.Identifiers[1].VerifiedAt.Equal(
			baseTime.Add(time.Minute).Truncate(time.Microsecond),
		) {
			t.Fatalf(
				"second identifier verified_at = %v, want %v",
				details.Identifiers[1].VerifiedAt,
				baseTime.Add(time.Minute).Truncate(time.Microsecond),
			)
		}
	})

	t.Run("returns suspended status", func(t *testing.T) {
		identityID := createIdentity(
			t,
			auth.IdentityStatusSuspended,
		)

		details, found, err := reader.FindByID(
			ctx,
			identityID,
		)
		if err != nil {
			t.Fatalf(
				"FindByID() returned an error: %v",
				err,
			)
		}

		if !found {
			t.Fatal("suspended identity was not found")
		}

		if details.Status != auth.IdentityStatusSuspended {
			t.Fatalf(
				"identity status = %q, want %q",
				details.Status,
				auth.IdentityStatusSuspended,
			)
		}
	})

	t.Run("returns identity without identifiers", func(t *testing.T) {
		identityID := createIdentity(
			t,
			auth.IdentityStatusDisabled,
		)

		details, found, err := reader.FindByID(
			ctx,
			identityID,
		)
		if err != nil {
			t.Fatalf(
				"FindByID() returned an error: %v",
				err,
			)
		}

		if !found {
			t.Fatal("identity without identifiers was not found")
		}

		if details.Status != auth.IdentityStatusDisabled {
			t.Fatalf(
				"identity status = %q, want %q",
				details.Status,
				auth.IdentityStatusDisabled,
			)
		}

		if len(details.Identifiers) != 0 {
			t.Fatalf(
				"identifiers count = %d, want 0",
				len(details.Identifiers),
			)
		}
	})

	t.Run("returns not found for unknown identity", func(t *testing.T) {
		details, found, err := reader.FindByID(
			ctx,
			"00000000-0000-0000-0000-000000000000",
		)
		if err != nil {
			t.Fatalf(
				"FindByID() returned an error: %v",
				err,
			)
		}

		if found {
			t.Fatal("unknown identity unexpectedly found")
		}

		if details.ID != "" {
			t.Fatalf(
				"unknown identity returned ID %q",
				details.ID,
			)
		}
	})
}

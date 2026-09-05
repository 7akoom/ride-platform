//go:build integration

package token

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/database"
	postgresrepo "github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/persistence/postgres"
	valkeyrepo "github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/persistence/valkey"
)

type integrationSessionAccessClock struct {
	now time.Time
}

func (c *integrationSessionAccessClock) Now() time.Time {
	return c.now
}

func TestSessionAccessCheckerRejectsPostgresRevokedSessionAndRebuildsMissingValkeyMarker(
	t *testing.T,
) {
	databaseURL := os.Getenv("DATABASE_URL")
	valkeyAddress := os.Getenv("VALKEY_ADDRESS")
	valkeyPassword := os.Getenv("VALKEY_PASSWORD")

	if databaseURL == "" ||
		valkeyAddress == "" ||
		valkeyPassword == "" {
		t.Skip(
			"required PostgreSQL or Valkey environment variables are not configured",
		)
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancel()

	pool, err := database.NewPostgresPool(
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

	valkeyClient, err := database.NewValkeyClient(
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
	t.Cleanup(valkeyClient.Close)

	const phoneNumber = "+9647500000053"

	_, err = pool.Exec(
		ctx,
		`
			DELETE FROM identities AS i
			USING identity_identifiers AS ii
			WHERE ii.identity_id = i.id
			AND ii.identifier_type = 'phone'
			AND ii.normalized_value = $1
		`,
		phoneNumber,
	)
	if err != nil {
		t.Fatalf(
			"clean existing test identity: %v",
			err,
		)
	}

	t.Cleanup(func() {
		_, cleanupErr := pool.Exec(
			context.Background(),
			`
				DELETE FROM identities AS i
				USING identity_identifiers AS ii
				WHERE ii.identity_id = i.id
				AND ii.identifier_type = 'phone'
				AND ii.normalized_value = $1
			`,
			phoneNumber,
		)
		if cleanupErr != nil {
			t.Errorf(
				"clean test identity: %v",
				cleanupErr,
			)
		}
	})

	now := time.Now().
		UTC().
		Truncate(time.Microsecond)

	var identityID string

	err = pool.QueryRow(
		ctx,
		`
			WITH created_identity AS (
				INSERT INTO identities (
					created_at,
					updated_at
				)
				VALUES ($1, $1)
				RETURNING id
			)
			INSERT INTO identity_identifiers (
				identity_id,
				identifier_type,
				normalized_value,
				verified_at,
				created_at,
				updated_at
			)
			SELECT
				id,
				'phone',
				$2,
				$1,
				$1,
				$1
			FROM created_identity
			RETURNING identity_id::text
		`,
		now,
		phoneNumber,
	).Scan(
		&identityID,
	)
	if err != nil {
		t.Fatalf(
			"create test identity: %v",
			err,
		)
	}

	sessionExpiresAt := now.Add(
		30 * time.Minute,
	)

	revokedAt := now

	var sessionID string

	err = pool.QueryRow(
		ctx,
		`
			INSERT INTO auth_sessions (
				identity_id,
				expires_at,
				revoked_at,
				created_at,
				updated_at
			)
			VALUES (
				$1::uuid,
				$2,
				$3,
				$3,
				$3
			)
			RETURNING id::text
		`,
		identityID,
		sessionExpiresAt,
		revokedAt,
	).Scan(
		&sessionID,
	)
	if err != nil {
		t.Fatalf(
			"create revoked authentication session: %v",
			err,
		)
	}

	revocationKey :=
		"auth:revoked-session:" + sessionID

	err = valkeyClient.Do(
		ctx,
		valkeyClient.B().
			Del().
			Key(revocationKey).
			Build(),
	).Error()
	if err != nil {
		t.Fatalf(
			"delete existing Valkey revocation marker: %v",
			err,
		)
	}

	t.Cleanup(func() {
		_ = valkeyClient.Do(
			context.Background(),
			valkeyClient.B().
				Del().
				Key(revocationKey).
				Build(),
		).Error()
	})

	revocationStore :=
		valkeyrepo.NewSessionAccessRevocationStore(
			valkeyClient,
		)

	stateStore :=
		postgresrepo.NewSessionRevocationStore(
			pool,
		)

	checker, err := NewSessionAccessChecker(
		revocationStore,
		stateStore,
		&integrationSessionAccessClock{
			now: now,
		},
	)
	if err != nil {
		t.Fatalf(
			"NewSessionAccessChecker() returned an error: %v",
			err,
		)
	}

	markerExistsBefore, err :=
		revocationStore.IsRevoked(
			ctx,
			sessionID,
		)
	if err != nil {
		t.Fatalf(
			"check marker before verification: %v",
			err,
		)
	}

	if markerExistsBefore {
		t.Fatal(
			"revocation marker unexpectedly existed before checker lookup",
		)
	}

	revoked, err := checker.IsRevoked(
		ctx,
		sessionID,
	)
	if err != nil {
		t.Fatalf(
			"IsRevoked() returned an error: %v",
			err,
		)
	}

	if !revoked {
		t.Fatal(
			"PostgreSQL-revoked session was unexpectedly accepted",
		)
	}

	markerExistsAfter, err :=
		revocationStore.IsRevoked(
			ctx,
			sessionID,
		)
	if err != nil {
		t.Fatalf(
			"check marker after verification: %v",
			err,
		)
	}

	if !markerExistsAfter {
		t.Fatal(
			"revocation marker was not rebuilt in Valkey",
		)
	}
}

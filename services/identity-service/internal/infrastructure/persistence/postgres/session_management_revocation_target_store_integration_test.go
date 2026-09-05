//go:build integration

package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/database"
)

func TestSessionRevocationStoreFindsSessionOnlyForOwningIdentity(
	t *testing.T,
) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("DATABASE_URL is required for integration test")
	}

	ctx := context.Background()

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

	now := time.Now().
		UTC().
		Truncate(time.Microsecond)

	var ownerIdentityID string

	err = pool.QueryRow(
		ctx,
		`
			INSERT INTO identities (
				created_at,
				updated_at
			)
			VALUES ($1, $1)
			RETURNING id::text
		`,
		now,
	).Scan(
		&ownerIdentityID,
	)
	if err != nil {
		t.Fatalf(
			"create owner identity: %v",
			err,
		)
	}

	var otherIdentityID string

	err = pool.QueryRow(
		ctx,
		`
			INSERT INTO identities (
				created_at,
				updated_at
			)
			VALUES ($1, $1)
			RETURNING id::text
		`,
		now,
	).Scan(
		&otherIdentityID,
	)
	if err != nil {
		t.Fatalf(
			"create other identity: %v",
			err,
		)
	}

	t.Cleanup(func() {
		for _, identityID := range []string{
			ownerIdentityID,
			otherIdentityID,
		} {
			_, cleanupErr := pool.Exec(
				context.Background(),
				`
					DELETE FROM identities
					WHERE id = $1::uuid
				`,
				identityID,
			)
			if cleanupErr != nil {
				t.Errorf(
					"clean test identity %q: %v",
					identityID,
					cleanupErr,
				)
			}
		}
	})

	sessionExpiresAt := now.Add(
		30 * 24 * time.Hour,
	)

	var sessionID string

	err = pool.QueryRow(
		ctx,
		`
			INSERT INTO auth_sessions (
				identity_id,
				expires_at,
				created_at,
				updated_at
			)
			VALUES (
				$1::uuid,
				$2,
				$3,
				$3
			)
			RETURNING id::text
		`,
		ownerIdentityID,
		sessionExpiresAt,
		now,
	).Scan(
		&sessionID,
	)
	if err != nil {
		t.Fatalf(
			"create authentication session: %v",
			err,
		)
	}

	store := NewSessionRevocationStore(
		pool,
	)

	target, found, err :=
		store.FindRevocationTargetByIdentityAndSessionID(
			ctx,
			ownerIdentityID,
			sessionID,
		)
	if err != nil {
		t.Fatalf(
			"FindRevocationTargetByIdentityAndSessionID() returned an error: %v",
			err,
		)
	}

	if !found {
		t.Fatal(
			"FindRevocationTargetByIdentityAndSessionID() did not find owned session",
		)
	}

	if target.SessionID != sessionID {
		t.Errorf(
			"target session ID = %q, expected %q",
			target.SessionID,
			sessionID,
		)
	}

	if !target.SessionExpiresAt.Equal(
		sessionExpiresAt,
	) {
		t.Errorf(
			"target expiration = %v, expected %v",
			target.SessionExpiresAt,
			sessionExpiresAt,
		)
	}

	foreignTarget, foreignFound, err :=
		store.FindRevocationTargetByIdentityAndSessionID(
			ctx,
			otherIdentityID,
			sessionID,
		)
	if err != nil {
		t.Fatalf(
			"foreign identity lookup returned an error: %v",
			err,
		)
	}

	if foreignFound {
		t.Fatalf(
			"foreign identity unexpectedly found session: %+v",
			foreignTarget,
		)
	}
}

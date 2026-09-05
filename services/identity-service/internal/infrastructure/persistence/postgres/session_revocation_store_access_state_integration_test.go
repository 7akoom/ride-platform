//go:build integration

package postgres

import (
	"testing"
	"time"
)

func TestSessionRevocationStoreFindsSessionAccessState(
	t *testing.T,
) {
	fixture := newSessionRevocationIntegrationFixture(
		t,
		"+9647500000052",
	)

	store := NewSessionRevocationStore(
		fixture.pool,
	)

	activeState, found, err :=
		store.FindSessionAccessState(
			fixture.ctx,
			fixture.sessionID,
		)
	if err != nil {
		t.Fatalf(
			"FindSessionAccessState() for active session returned an error: %v",
			err,
		)
	}

	if !found {
		t.Fatal(
			"FindSessionAccessState() did not find active session",
		)
	}

	if activeState.Revoked {
		t.Fatal(
			"active session was unexpectedly reported as revoked",
		)
	}

	if !activeState.SessionExpiresAt.Equal(
		fixture.sessionExpiresAt,
	) {
		t.Fatalf(
			"active session expiration = %v, expected %v",
			activeState.SessionExpiresAt,
			fixture.sessionExpiresAt,
		)
	}

	revokedAt := fixture.now.Add(
		time.Minute,
	)

	_, err = fixture.pool.Exec(
		fixture.ctx,
		`
			UPDATE auth_sessions
			SET
				revoked_at = $2,
				updated_at = $2
			WHERE id = $1::uuid
		`,
		fixture.sessionID,
		revokedAt,
	)
	if err != nil {
		t.Fatalf(
			"revoke authentication session: %v",
			err,
		)
	}

	revokedState, found, err :=
		store.FindSessionAccessState(
			fixture.ctx,
			fixture.sessionID,
		)
	if err != nil {
		t.Fatalf(
			"FindSessionAccessState() for revoked session returned an error: %v",
			err,
		)
	}

	if !found {
		t.Fatal(
			"FindSessionAccessState() did not find revoked session",
		)
	}

	if !revokedState.Revoked {
		t.Fatal(
			"revoked session was unexpectedly reported as active",
		)
	}

	if !revokedState.SessionExpiresAt.Equal(
		fixture.sessionExpiresAt,
	) {
		t.Fatalf(
			"revoked session expiration = %v, expected %v",
			revokedState.SessionExpiresAt,
			fixture.sessionExpiresAt,
		)
	}

	const missingSessionID = "ffffffff-ffff-4fff-8fff-ffffffffffff"

	missingState, found, err :=
		store.FindSessionAccessState(
			fixture.ctx,
			missingSessionID,
		)
	if err != nil {
		t.Fatalf(
			"FindSessionAccessState() for missing session returned an error: %v",
			err,
		)
	}

	if found {
		t.Fatal(
			"FindSessionAccessState() unexpectedly found missing session",
		)
	}

	if missingState.Revoked ||
		!missingState.SessionExpiresAt.IsZero() {
		t.Fatalf(
			"missing session state = %+v, expected zero value",
			missingState,
		)
	}
}

//go:build integration

package postgres

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/database"
)

func TestAllSessionsRevocationStoreRevokesAllSessionsAndOldTokenCannotRevokeNewSession(
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

	const phoneNumber = "+9647500000052"

	_, err = pool.Exec(
		ctx,
		`
			DELETE FROM identities
			WHERE phone_number = $1
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
				DELETE FROM identities
				WHERE phone_number = $1
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
			INSERT INTO identities (
				phone_number,
				created_at,
				updated_at
			)
			VALUES (
				$1,
				$2,
				$2
			)
			RETURNING id::text
		`,
		phoneNumber,
		now,
	).Scan(
		&identityID,
	)
	if err != nil {
		t.Fatalf(
			"create test identity: %v",
			err,
		)
	}

	var firstSessionID string

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
		identityID,
		now.Add(30*24*time.Hour),
		now,
	).Scan(
		&firstSessionID,
	)
	if err != nil {
		t.Fatalf(
			"create first authentication session: %v",
			err,
		)
	}

	secondSessionCreatedAt := now.Add(
		time.Second,
	)

	var secondSessionID string

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
		identityID,
		secondSessionCreatedAt.Add(
			30*24*time.Hour,
		),
		secondSessionCreatedAt,
	).Scan(
		&secondSessionID,
	)
	if err != nil {
		t.Fatalf(
			"create second authentication session: %v",
			err,
		)
	}

	firstTokenHash := strings.Repeat(
		"f",
		64,
	)

	secondTokenHash := strings.Repeat(
		"1",
		64,
	)

	_, err = pool.Exec(
		ctx,
		`
			INSERT INTO refresh_tokens (
				session_id,
				token_hash,
				expires_at,
				created_at
			)
			VALUES (
				$1::uuid,
				$2,
				$3,
				$4
			)
		`,
		firstSessionID,
		firstTokenHash,
		now.Add(29*24*time.Hour),
		now,
	)
	if err != nil {
		t.Fatalf(
			"create first refresh token: %v",
			err,
		)
	}

	_, err = pool.Exec(
		ctx,
		`
			INSERT INTO refresh_tokens (
				session_id,
				token_hash,
				expires_at,
				created_at
			)
			VALUES (
				$1::uuid,
				$2,
				$3,
				$4
			)
		`,
		secondSessionID,
		secondTokenHash,
		secondSessionCreatedAt.Add(
			29*24*time.Hour,
		),
		secondSessionCreatedAt,
	)
	if err != nil {
		t.Fatalf(
			"create second refresh token: %v",
			err,
		)
	}

	store := NewAllSessionsRevocationStore(
		pool,
	)

	logoutAllAt := now.Add(
		time.Minute,
	)

	err = store.RevokeAllByRefreshTokenHash(
		ctx,
		firstTokenHash,
		logoutAllAt,
	)
	if err != nil {
		t.Fatalf(
			"RevokeAllByRefreshTokenHash() returned an error: %v",
			err,
		)
	}

	var (
		totalSessions   int
		revokedSessions int
		activeSessions  int
	)

	err = pool.QueryRow(
		ctx,
		`
			SELECT
				COUNT(*),
				COUNT(*) FILTER (
					WHERE revoked_at IS NOT NULL
				),
				COUNT(*) FILTER (
					WHERE revoked_at IS NULL
				)
			FROM auth_sessions
			WHERE identity_id = $1::uuid
		`,
		identityID,
	).Scan(
		&totalSessions,
		&revokedSessions,
		&activeSessions,
	)
	if err != nil {
		t.Fatalf(
			"query authentication sessions: %v",
			err,
		)
	}

	if totalSessions != 2 {
		t.Fatalf(
			"session count = %d, expected 2",
			totalSessions,
		)
	}

	if revokedSessions != 2 {
		t.Fatalf(
			"revoked session count = %d, expected 2",
			revokedSessions,
		)
	}

	if activeSessions != 0 {
		t.Fatalf(
			"active session count = %d, expected 0",
			activeSessions,
		)
	}

	var (
		totalRefreshTokens   int
		revokedRefreshTokens int
		activeRefreshTokens  int
	)

	err = pool.QueryRow(
		ctx,
		`
			SELECT
				COUNT(*),
				COUNT(*) FILTER (
					WHERE rt.revoked_at IS NOT NULL
				),
				COUNT(*) FILTER (
					WHERE rt.revoked_at IS NULL
				)
			FROM refresh_tokens AS rt
			INNER JOIN auth_sessions AS s
				ON s.id = rt.session_id
			WHERE s.identity_id = $1::uuid
		`,
		identityID,
	).Scan(
		&totalRefreshTokens,
		&revokedRefreshTokens,
		&activeRefreshTokens,
	)
	if err != nil {
		t.Fatalf(
			"query identity refresh tokens: %v",
			err,
		)
	}

	if totalRefreshTokens != 2 {
		t.Fatalf(
			"refresh token count = %d, expected 2",
			totalRefreshTokens,
		)
	}

	if revokedRefreshTokens != 2 {
		t.Fatalf(
			"revoked refresh token count = %d, expected 2",
			revokedRefreshTokens,
		)
	}

	if activeRefreshTokens != 0 {
		t.Fatalf(
			"active refresh token count = %d, expected 0",
			activeRefreshTokens,
		)
	}

	newSessionCreatedAt := logoutAllAt.Add(
		time.Minute,
	)

	var newSessionID string

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
		identityID,
		newSessionCreatedAt.Add(
			30*24*time.Hour,
		),
		newSessionCreatedAt,
	).Scan(
		&newSessionID,
	)
	if err != nil {
		t.Fatalf(
			"create new authentication session: %v",
			err,
		)
	}

	newTokenHash := strings.Repeat(
		"2",
		64,
	)

	_, err = pool.Exec(
		ctx,
		`
			INSERT INTO refresh_tokens (
				session_id,
				token_hash,
				expires_at,
				created_at
			)
			VALUES (
				$1::uuid,
				$2,
				$3,
				$4
			)
		`,
		newSessionID,
		newTokenHash,
		newSessionCreatedAt.Add(
			29*24*time.Hour,
		),
		newSessionCreatedAt,
	)
	if err != nil {
		t.Fatalf(
			"create new refresh token: %v",
			err,
		)
	}

	err = store.RevokeAllByRefreshTokenHash(
		ctx,
		firstTokenHash,
		newSessionCreatedAt.Add(time.Minute),
	)
	if err != nil {
		t.Fatalf(
			"old refresh token returned an error: %v",
			err,
		)
	}

	var newSessionRevokedAt *time.Time

	err = pool.QueryRow(
		ctx,
		`
			SELECT revoked_at
			FROM auth_sessions
			WHERE id = $1::uuid
		`,
		newSessionID,
	).Scan(
		&newSessionRevokedAt,
	)
	if err != nil {
		t.Fatalf(
			"query new authentication session: %v",
			err,
		)
	}

	if newSessionRevokedAt != nil {
		t.Fatal(
			"old refresh token revoked a newly created session",
		)
	}

	var newTokenRevokedAt *time.Time

	err = pool.QueryRow(
		ctx,
		`
			SELECT revoked_at
			FROM refresh_tokens
			WHERE token_hash = $1
		`,
		newTokenHash,
	).Scan(
		&newTokenRevokedAt,
	)
	if err != nil {
		t.Fatalf(
			"query new refresh token: %v",
			err,
		)
	}

	if newTokenRevokedAt != nil {
		t.Fatal(
			"old refresh token revoked a newly created refresh token",
		)
	}
}

func TestAllSessionsRevocationStoreRevokesOnlySnapshotSessions(
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

	const phoneNumber = "+9647500000053"

	_, err = pool.Exec(
		ctx,
		`
			DELETE FROM identities
			WHERE phone_number = $1
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
				DELETE FROM identities
				WHERE phone_number = $1
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
			INSERT INTO identities (
				phone_number,
				created_at,
				updated_at
			)
			VALUES (
				$1,
				$2,
				$2
			)
			RETURNING id::text
		`,
		phoneNumber,
		now,
	).Scan(
		&identityID,
	)
	if err != nil {
		t.Fatalf(
			"create test identity: %v",
			err,
		)
	}

	createSession := func(
		createdAt time.Time,
		tokenHash string,
	) string {
		t.Helper()

		var sessionID string

		err := pool.QueryRow(
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
			identityID,
			createdAt.Add(30*24*time.Hour),
			createdAt,
		).Scan(
			&sessionID,
		)
		if err != nil {
			t.Fatalf(
				"create authentication session: %v",
				err,
			)
		}

		_, err = pool.Exec(
			ctx,
			`
				INSERT INTO refresh_tokens (
					session_id,
					token_hash,
					expires_at,
					created_at
				)
				VALUES (
					$1::uuid,
					$2,
					$3,
					$4
				)
			`,
			sessionID,
			tokenHash,
			createdAt.Add(29*24*time.Hour),
			createdAt,
		)
		if err != nil {
			t.Fatalf(
				"create refresh token: %v",
				err,
			)
		}

		return sessionID
	}

	firstTokenHash := strings.Repeat(
		"a",
		64,
	)

	secondTokenHash := strings.Repeat(
		"b",
		64,
	)

	thirdTokenHash := strings.Repeat(
		"c",
		64,
	)

	firstSessionID := createSession(
		now,
		firstTokenHash,
	)

	secondSessionID := createSession(
		now.Add(time.Second),
		secondTokenHash,
	)

	store := NewAllSessionsRevocationStore(
		pool,
	)

	snapshotAt := now.Add(
		time.Minute,
	)

	target, found, err :=
		store.FindAllSessionRevocationTargetsByRefreshTokenHash(
			ctx,
			firstTokenHash,
			snapshotAt,
		)
	if err != nil {
		t.Fatalf(
			"FindAllSessionRevocationTargetsByRefreshTokenHash() returned an error: %v",
			err,
		)
	}

	if !found {
		t.Fatal(
			"expected all sessions revocation target to be found",
		)
	}

	if target.IdentityID != identityID {
		t.Fatalf(
			"target identity ID = %q, expected %q",
			target.IdentityID,
			identityID,
		)
	}

	if len(target.Sessions) != 2 {
		t.Fatalf(
			"snapshot session count = %d, expected 2",
			len(target.Sessions),
		)
	}

	snapshotSessionIDs := make(
		[]string,
		0,
		len(target.Sessions),
	)

	snapshotSessionSet := make(
		map[string]bool,
		len(target.Sessions),
	)

	for _, session := range target.Sessions {
		snapshotSessionIDs = append(
			snapshotSessionIDs,
			session.SessionID,
		)

		snapshotSessionSet[session.SessionID] = true
	}

	if !snapshotSessionSet[firstSessionID] {
		t.Fatal(
			"first session is missing from revocation snapshot",
		)
	}

	if !snapshotSessionSet[secondSessionID] {
		t.Fatal(
			"second session is missing from revocation snapshot",
		)
	}

	thirdSessionCreatedAt := snapshotAt.Add(
		time.Minute,
	)

	thirdSessionID := createSession(
		thirdSessionCreatedAt,
		thirdTokenHash,
	)

	revokeAt := thirdSessionCreatedAt.Add(
		time.Minute,
	)

	err = store.RevokeSessions(
		ctx,
		target.IdentityID,
		snapshotSessionIDs,
		revokeAt,
	)
	if err != nil {
		t.Fatalf(
			"RevokeSessions() returned an error: %v",
			err,
		)
	}

	type revocationState struct {
		sessionRevokedAt *time.Time
		tokenRevokedAt   *time.Time
	}

	readRevocationState := func(
		sessionID string,
		tokenHash string,
	) revocationState {
		t.Helper()

		var state revocationState

		err := pool.QueryRow(
			ctx,
			`
				SELECT
					s.revoked_at,
					rt.revoked_at
				FROM auth_sessions AS s
				INNER JOIN refresh_tokens AS rt
					ON rt.session_id = s.id
				WHERE s.id = $1::uuid
				  AND rt.token_hash = $2
			`,
			sessionID,
			tokenHash,
		).Scan(
			&state.sessionRevokedAt,
			&state.tokenRevokedAt,
		)
		if err != nil {
			t.Fatalf(
				"query revocation state: %v",
				err,
			)
		}

		return state
	}

	firstState := readRevocationState(
		firstSessionID,
		firstTokenHash,
	)

	secondState := readRevocationState(
		secondSessionID,
		secondTokenHash,
	)

	thirdState := readRevocationState(
		thirdSessionID,
		thirdTokenHash,
	)

	if firstState.sessionRevokedAt == nil ||
		firstState.tokenRevokedAt == nil {
		t.Fatal(
			"first snapshot session and refresh token were not revoked",
		)
	}

	if secondState.sessionRevokedAt == nil ||
		secondState.tokenRevokedAt == nil {
		t.Fatal(
			"second snapshot session and refresh token were not revoked",
		)
	}

	if thirdState.sessionRevokedAt != nil {
		t.Fatal(
			"session created after snapshot was revoked",
		)
	}

	if thirdState.tokenRevokedAt != nil {
		t.Fatal(
			"refresh token created after snapshot was revoked",
		)
	}
}

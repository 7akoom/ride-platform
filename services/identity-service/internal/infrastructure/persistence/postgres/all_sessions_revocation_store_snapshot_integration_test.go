//go:build integration

package postgres

import (
	"strings"
	"testing"
	"time"
)

func TestAllSessionsRevocationStoreRevokesOnlySnapshotSessions(
	t *testing.T,
) {
	fixture := newAllSessionsRevocationTestFixture(
		t,
		"+9647500000153",
	)

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

	firstSessionID :=
		fixture.createSessionWithRefreshToken(
			fixture.now,
			firstTokenHash,
		)

	secondSessionID :=
		fixture.createSessionWithRefreshToken(
			fixture.now.Add(time.Second),
			secondTokenHash,
		)

	store := NewAllSessionsRevocationStore(
		fixture.pool,
	)

	snapshotAt := fixture.now.Add(
		time.Minute,
	)

	target, found, err :=
		store.FindAllSessionRevocationTargetsByRefreshTokenHash(
			fixture.ctx,
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

	if target.IdentityID != fixture.identityID {
		t.Fatalf(
			"target identity ID = %q, expected %q",
			target.IdentityID,
			fixture.identityID,
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

	thirdSessionID :=
		fixture.createSessionWithRefreshToken(
			thirdSessionCreatedAt,
			thirdTokenHash,
		)

	revokeAt := thirdSessionCreatedAt.Add(
		time.Minute,
	)

	err = store.RevokeSessions(
		fixture.ctx,
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

	firstState := fixture.readRevocationState(
		firstSessionID,
		firstTokenHash,
	)

	secondState := fixture.readRevocationState(
		secondSessionID,
		secondTokenHash,
	)

	thirdState := fixture.readRevocationState(
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

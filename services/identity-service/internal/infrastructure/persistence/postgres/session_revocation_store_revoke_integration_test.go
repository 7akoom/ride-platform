//go:build integration

package postgres

import (
	"strings"
	"testing"
	"time"
)

func TestSessionRevocationStoreRevokesSessionAndAllRefreshTokens(
	t *testing.T,
) {
	fixture := newSessionRevocationIntegrationFixture(
		t,
		"+9647500000051",
	)

	firstTokenHash := strings.Repeat(
		"c",
		64,
	)

	secondTokenHash := strings.Repeat(
		"d",
		64,
	)

	refreshExpiresAt := fixture.now.Add(
		29 * 24 * time.Hour,
	)

	fixture.createRefreshToken(
		firstTokenHash,
		refreshExpiresAt,
	)

	fixture.createRefreshToken(
		secondTokenHash,
		refreshExpiresAt,
	)

	store := NewSessionRevocationStore(
		fixture.pool,
	)

	target, found, err :=
		store.FindRevocationTargetByRefreshTokenHash(
			fixture.ctx,
			firstTokenHash,
		)
	if err != nil {
		t.Fatalf(
			"FindRevocationTargetByRefreshTokenHash() returned an error: %v",
			err,
		)
	}

	if !found {
		t.Fatal(
			"FindRevocationTargetByRefreshTokenHash() did not find known refresh token",
		)
	}

	if target.SessionID != fixture.sessionID {
		t.Fatalf(
			"revocation target session ID = %q, expected %q",
			target.SessionID,
			fixture.sessionID,
		)
	}

	if !target.SessionExpiresAt.Equal(
		fixture.sessionExpiresAt,
	) {
		t.Fatalf(
			"revocation target expiration = %v, expected %v",
			target.SessionExpiresAt,
			fixture.sessionExpiresAt,
		)
	}

	unknownTokenHash := strings.Repeat(
		"e",
		64,
	)

	unknownTarget, found, err :=
		store.FindRevocationTargetByRefreshTokenHash(
			fixture.ctx,
			unknownTokenHash,
		)
	if err != nil {
		t.Fatalf(
			"unknown refresh token lookup returned an error: %v",
			err,
		)
	}

	if found {
		t.Fatal(
			"unknown refresh token lookup unexpectedly found a session",
		)
	}

	if unknownTarget.SessionID != "" ||
		!unknownTarget.SessionExpiresAt.IsZero() {
		t.Fatalf(
			"unknown refresh token target = %+v, expected zero value",
			unknownTarget,
		)
	}

	revokedAt := fixture.now.Add(
		time.Minute,
	)

	err = store.RevokeByRefreshTokenHash(
		fixture.ctx,
		firstTokenHash,
		revokedAt,
	)
	if err != nil {
		t.Fatalf(
			"RevokeByRefreshTokenHash() returned an error: %v",
			err,
		)
	}

	if fixture.sessionRevokedAt() == nil {
		t.Fatal(
			"authentication session was not revoked",
		)
	}

	totalRefreshTokens, revokedRefreshTokens, activeRefreshTokens :=
		fixture.refreshTokenCounts()

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

	err = store.RevokeByRefreshTokenHash(
		fixture.ctx,
		firstTokenHash,
		revokedAt.Add(time.Second),
	)
	if err != nil {
		t.Fatalf(
			"second logout returned an error: %v",
			err,
		)
	}

	err = store.RevokeByRefreshTokenHash(
		fixture.ctx,
		unknownTokenHash,
		revokedAt.Add(2*time.Second),
	)
	if err != nil {
		t.Fatalf(
			"unknown refresh token returned an error: %v",
			err,
		)
	}
}

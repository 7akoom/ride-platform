//go:build integration

package token

import (
	"testing"
	"time"
)

func TestSigningInCancelsAPendingAccountDeletion(t *testing.T) {
	fixture := newSessionStoreIntegrationFixture(t, "+9647500000071")

	if _, err := fixture.pool.Exec(fixture.ctx,
		`INSERT INTO account_deletions (identity_id, status, requested_at, purge_after)
		 VALUES ($1, 'pending', now(), now() + interval '30 days')`, fixture.identityID); err != nil {
		t.Fatalf("create pending deletion: %v", err)
	}

	var base time.Time
	if err := fixture.pool.QueryRow(fixture.ctx, "SELECT CURRENT_TIMESTAMP").Scan(&base); err != nil {
		t.Fatal(err)
	}

	challengeID := "otp_ch_cancels_deletion"
	fixture.createOTPChallenge(challengeID, "test-code-hash", base.UTC().Add(5*time.Minute))

	var now time.Time
	if err := fixture.pool.QueryRow(fixture.ctx, "SELECT CURRENT_TIMESTAMP").Scan(&now); err != nil {
		t.Fatal(err)
	}

	now = now.UTC()

	if _, err := NewSessionStore(fixture.pool).Create(fixture.ctx, SessionCreationInput{
		ChallengeID:           challengeID,
		VerifiedAt:            now,
		SessionID:             fixture.generateSessionID(),
		IdentityID:            fixture.identityID,
		SessionExpiresAt:      now.Add(30 * 24 * time.Hour),
		RefreshTokenHash:      "7a1e0000000000000000000000000000000000000000000000000000000d0e1e",
		RefreshTokenExpiresAt: now.Add(29 * 24 * time.Hour),
	}); err != nil {
		t.Fatalf("sign in: %v", err)
	}

	var status string
	if err := fixture.pool.QueryRow(fixture.ctx,
		`SELECT status FROM account_deletions WHERE identity_id = $1`, fixture.identityID).Scan(&status); err != nil || status != "cancelled" {
		t.Fatalf("deletion status %q, %v", status, err)
	}

	var events int
	if err := fixture.pool.QueryRow(fixture.ctx,
		`SELECT count(*) FROM outbox_events WHERE aggregate_id = $1 AND event_type = 'identity.deletion_cancelled'`,
		fixture.identityID).Scan(&events); err != nil || events != 1 {
		t.Fatalf("cancelled events %d, %v", events, err)
	}
}

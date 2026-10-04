//go:build integration

package postgres

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	"github.com/7akoom/ride-platform/services/identity-service/internal/application/deletion"
	databaseinfra "github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/database"
	"github.com/jackc/pgx/v5/pgxpool"
)

func accountDeletionPool(t *testing.T) (*pgxpool.Pool, context.Context) {
	t.Helper()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("DATABASE_URL is required for integration test")
	}

	ctx := context.Background()

	pool, err := databaseinfra.NewPostgresPool(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to PostgreSQL: %v", err)
	}

	t.Cleanup(pool.Close)

	return pool, ctx
}

func countRows(t *testing.T, pool *pgxpool.Pool, query string, args ...any) int {
	t.Helper()

	var n int
	if err := pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}

	return n
}

func TestAccountDeletionStoreLifecycle(t *testing.T) {
	pool, ctx := accountDeletionPool(t)
	store := NewAccountDeletionStore(pool)

	var identityID string
	if err := pool.QueryRow(ctx, `INSERT INTO identities (status) VALUES ('active') RETURNING id::text`).Scan(&identityID); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM identities WHERE id = $1`, identityID) })

	// A phone made from the identity id, so parallel runs never collide.
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}

		return '0' + (r-'a')%10
	}, strings.ReplaceAll(identityID, "-", ""))
	phone := "+9647" + digits[:9]

	if _, err := pool.Exec(ctx,
		`INSERT INTO identity_identifiers (identity_id, identifier_type, normalized_value, verified_at)
		 VALUES ($1, 'phone', $2, now())`, identityID, phone); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(ctx,
		`INSERT INTO auth_sessions (id, identity_id, expires_at) VALUES (gen_random_uuid(), $1, now() + interval '1 day')`,
		identityID); err != nil {
		t.Fatal(err)
	}

	if _, found, err := store.Find(ctx, identityID); err != nil || found {
		t.Fatalf("nothing asked yet: %v %v", found, err)
	}

	target := identityID
	now := time.Now().UTC().Truncate(time.Microsecond)

	challenge := func(id, value string) auth.OTPChallenge {
		return auth.OTPChallenge{
			ID: id, Identifier: auth.Identifier{Type: auth.IdentifierTypePhone, Value: value},
			Purpose: auth.OTPPurposeDeleteAccount, TargetIdentityID: &target,
			CodeHash: "hash", ExpiresAt: now.Add(5 * time.Minute),
		}
	}

	if err := store.CreateChallenge(ctx, challenge("del-other-"+identityID[:8], "+9647509999999")); !errors.Is(err, auth.ErrIdentifierNotLinked) {
		t.Fatalf("a phone that is not the account's: %v", err)
	}

	first, second := "del-1-"+identityID[:8], "del-2-"+identityID[:8]

	if err := store.CreateChallenge(ctx, challenge(first, phone)); err != nil {
		t.Fatal(err)
	}

	if err := store.CreateChallenge(ctx, challenge(second, phone)); err != nil {
		t.Fatal(err)
	}

	if n := countRows(t, pool, `SELECT count(*) FROM otp_challenges WHERE id = $1 AND cancelled_at IS NOT NULL`, first); n != 1 {
		t.Fatal("the earlier code was not cancelled")
	}

	// The challenge reads back like any other (the verify step looks it up).
	found, err := NewChallengeRepository(pool).FindByID(ctx, second)
	if err != nil || found.Purpose != auth.OTPPurposeDeleteAccount || found.TargetIdentityID == nil || *found.TargetIdentityID != identityID {
		t.Fatalf("read the deletion challenge back: %+v %v", found, err)
	}

	unlinkID := "unlink-" + identityID[:8]
	if _, err := pool.Exec(ctx,
		`INSERT INTO otp_challenges (id, identifier_type, normalized_value, purpose, target_identity_id, code_hash, expires_at)
		 VALUES ($1, 'phone', $2, 'unlink_identifier', $3, 'hash', now() + interval '5 minutes')`,
		unlinkID, phone, identityID); err != nil {
		t.Fatal(err)
	}

	if _, err := NewChallengeRepository(pool).FindByID(ctx, unlinkID); err != nil {
		t.Fatalf("an unlink challenge must read back too: %v", err)
	}

	// Confirmed after the challenge was created (the database checks it).
	now = time.Now().UTC().Add(time.Second).Truncate(time.Microsecond)
	purge := now.Add(30 * 24 * time.Hour)

	confirm := deletion.ConfirmInput{ChallengeID: second, IdentityID: identityID, VerifiedAt: now, PurgeAfter: purge}

	started, err := store.Confirm(ctx, confirm)
	if err != nil {
		t.Fatal(err)
	}

	if started.Status != deletion.StatusPending || !started.PurgeAfter.Equal(purge) {
		t.Fatalf("started %+v", started)
	}

	if _, err := store.Confirm(ctx, confirm); !errors.Is(err, auth.ErrChallengeUsed) {
		t.Fatalf("the same code twice: %v", err)
	}

	if n := countRows(t, pool, `SELECT count(*) FROM auth_sessions WHERE identity_id = $1 AND revoked_at IS NULL`, identityID); n != 0 {
		t.Fatal("sessions were not revoked")
	}

	if n := countRows(t, pool, `SELECT count(*) FROM outbox_events WHERE aggregate_id = $1 AND event_type = 'identity.deletion_requested'`, identityID); n != 1 {
		t.Fatal("no deletion_requested event")
	}

	if due, err := store.Due(ctx, now, 100); err != nil || containsDeletion(due, identityID) {
		t.Fatalf("not due yet: %v %v", due, err)
	}

	later := purge.Add(time.Minute)

	if due, err := store.Due(ctx, later, 100); err != nil || !containsDeletion(due, identityID) {
		t.Fatalf("due after the grace period: %v %v", due, err)
	}

	if err := store.Postpone(ctx, identityID, later.Add(time.Hour), "trip"); err != nil {
		t.Fatal(err)
	}

	if due, _ := store.Due(ctx, later, 100); containsDeletion(due, identityID) {
		t.Fatal("a postponed deletion is still due")
	}

	done, err := store.Complete(ctx, deletion.CompleteInput{IdentityID: identityID, RiderID: "", CompletedAt: later})
	if err != nil || !done {
		t.Fatalf("complete: %v %v", done, err)
	}

	if n := countRows(t, pool, `SELECT count(*) FROM identity_identifiers WHERE identity_id = $1`, identityID); n != 0 {
		t.Fatal("the phone is still there")
	}

	if n := countRows(t, pool, `SELECT count(*) FROM otp_challenges WHERE normalized_value = $1`, phone); n != 0 {
		t.Fatal("codes sent to the phone are still there")
	}

	if n := countRows(t, pool, `SELECT count(*) FROM auth_sessions WHERE identity_id = $1`, identityID); n != 0 {
		t.Fatal("sessions are still there")
	}

	if n := countRows(t, pool, `SELECT count(*) FROM identities WHERE id = $1 AND status = 'disabled'`, identityID); n != 1 {
		t.Fatal("the identity is not disabled")
	}

	if n := countRows(t, pool, `SELECT count(*) FROM outbox_events WHERE aggregate_id = $1 AND event_type = 'identity.deleted'`, identityID); n != 1 {
		t.Fatal("no identity.deleted event")
	}

	if done, err := store.Complete(ctx, deletion.CompleteInput{IdentityID: identityID, CompletedAt: later}); err != nil || done {
		t.Fatalf("a second complete: %v %v", done, err)
	}
}

func containsDeletion(list []deletion.Deletion, identityID string) bool {
	for _, d := range list {
		if d.IdentityID == identityID {
			return true
		}
	}

	return false
}

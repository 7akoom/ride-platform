//go:build integration

package postgres

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/dataexport"
	"github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/identifier"
)

func TestDataExportStoreLifecycle(t *testing.T) {
	pool, ctx := accountDeletionPool(t)
	store := NewDataExportStore(pool)
	ids := identifier.NewUUIDGenerator()

	var identityID string
	if err := pool.QueryRow(ctx, `INSERT INTO identities (status) VALUES ('active') RETURNING id::text`).Scan(&identityID); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM identities WHERE id = $1`, identityID) })

	if _, err := pool.Exec(ctx,
		`INSERT INTO identity_identifiers (identity_id, identifier_type, normalized_value, verified_at)
		 VALUES ($1::uuid, 'phone', '+9647500' || lpad(floor(random() * 1000000)::int::text, 6, '0'), now())`,
		identityID); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	export := dataexport.Export{ID: ids.NewID(), IdentityID: identityID, Status: dataexport.StatusPending, RequestedAt: now}

	if err := store.Create(ctx, export); err != nil {
		t.Fatal(err)
	}

	claimed, err := store.ClaimPending(ctx, now.Add(time.Second), 10*time.Minute, 50)
	if err != nil || !containsExport(claimed, export.ID) {
		t.Fatalf("claim: %v %v", claimed, err)
	}

	if again, _ := store.ClaimPending(ctx, now.Add(2*time.Second), 10*time.Minute, 50); containsExport(again, export.ID) {
		t.Fatal("a claimed export was claimed again within its lease")
	}

	mediaID := ids.NewID()
	if err := store.MarkReady(ctx, dataexport.ReadyInput{
		ID: export.ID, MediaID: mediaID, SizeBytes: 1234, ReadyAt: now, ExpiresAt: now.Add(time.Hour),
		Profiles: dataexport.Profiles{RiderID: ids.NewID()},
	}); err != nil {
		t.Fatal(err)
	}

	found, err := store.Find(ctx, identityID, export.ID)
	if err != nil || found.Status != dataexport.StatusReady || found.MediaID != mediaID || found.SizeBytes != 1234 {
		t.Fatalf("found %+v %v", found, err)
	}

	if _, err := store.Find(ctx, ids.NewID(), export.ID); err != dataexport.ErrNotFound {
		t.Fatalf("someone else's export: %v", err)
	}

	if n := countRows(t, pool, `SELECT count(*) FROM outbox_events WHERE aggregate_id = $1 AND event_type = 'identity.data_export_ready'`, identityID); n != 1 {
		t.Fatal("no data_export_ready event")
	}

	expired, err := store.ListExpired(ctx, now.Add(2*time.Hour), 50)
	if err != nil || !containsExport(expired, export.ID) {
		t.Fatalf("expired: %v %v", expired, err)
	}

	if err := store.MarkExpired(ctx, export.ID); err != nil {
		t.Fatal(err)
	}

	sections, err := store.Sections(ctx, identityID, dataexport.Profiles{})
	if err != nil || len(sections) != len(identitySections) {
		t.Fatalf("sections %v %v", sections, err)
	}

	for _, s := range sections {
		var rows []map[string]any
		if err := json.Unmarshal(s.Content, &rows); err != nil {
			t.Fatalf("%s: %v", s.Name, err)
		}

		if s.Name == "identity/sign_in_methods.json" && len(rows) != 1 {
			t.Fatalf("sign-in methods %v", rows)
		}
	}
}

func containsExport(list []dataexport.Export, id string) bool {
	for _, e := range list {
		if e.ID == id {
			return true
		}
	}

	return false
}

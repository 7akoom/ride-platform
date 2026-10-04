package dataexport

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

const person = "11111111-1111-4111-8111-111111111111"

var now = time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

type seqIDs struct{ n int }

func (s *seqIDs) NewID() string {
	s.n++

	return strings.Repeat(string(rune('a'+s.n)), 8) + "-0000-4000-8000-000000000000"
}

type memStore struct {
	exports []Export
	ready   []ReadyInput
	failed  map[string]int
	expired []string
}

func (m *memStore) Create(_ context.Context, e Export) error {
	m.exports = append([]Export{e}, m.exports...)

	return nil
}

func (m *memStore) List(_ context.Context, _ string, limit int) ([]Export, error) {
	if len(m.exports) > limit {
		return m.exports[:limit], nil
	}

	return m.exports, nil
}

func (m *memStore) Find(_ context.Context, identityID, id string) (Export, error) {
	for _, e := range m.exports {
		if e.ID == id && e.IdentityID == identityID {
			return e, nil
		}
	}

	return Export{}, ErrNotFound
}

func (m *memStore) ClaimPending(context.Context, time.Time, time.Duration, int) ([]Export, error) {
	var out []Export

	for _, e := range m.exports {
		if e.Status == StatusPending {
			out = append(out, e)
		}
	}

	return out, nil
}

func (m *memStore) set(id string, change func(*Export)) {
	for i := range m.exports {
		if m.exports[i].ID == id {
			change(&m.exports[i])
		}
	}
}

func (m *memStore) MarkReady(_ context.Context, in ReadyInput) error {
	m.ready = append(m.ready, in)
	m.set(in.ID, func(e *Export) {
		e.Status, e.MediaID, e.ReadyAt, e.ExpiresAt = StatusReady, in.MediaID, &in.ReadyAt, &in.ExpiresAt
	})

	return nil
}

func (m *memStore) MarkAttemptFailed(_ context.Context, id string, retryAt *time.Time, _ string) error {
	if m.failed == nil {
		m.failed = map[string]int{}
	}

	m.failed[id]++
	m.set(id, func(e *Export) {
		e.Attempts++
		if retryAt == nil {
			e.Status = StatusFailed
		}
	})

	return nil
}

func (m *memStore) ListExpired(_ context.Context, at time.Time, _ int) ([]Export, error) {
	var out []Export

	for _, e := range m.exports {
		if e.Status == StatusReady && !e.ExpiresAt.After(at) {
			out = append(out, e)
		}
	}

	return out, nil
}

func (m *memStore) MarkExpired(_ context.Context, id string) error {
	m.expired = append(m.expired, id)
	m.set(id, func(e *Export) { e.Status = StatusExpired })

	return nil
}

type profiles struct{}

func (profiles) Profiles(context.Context, string) (Profiles, error) {
	return Profiles{RiderID: "rider-1"}, nil
}

type memFiles struct {
	stored  [][]byte
	deleted []string
}

func (f *memFiles) Store(_ context.Context, _ string, zip []byte) (string, error) {
	f.stored = append(f.stored, zip)

	return "media-1", nil
}

func (f *memFiles) DownloadURL(context.Context, string) (string, time.Time, error) {
	return "https://files/x", now.Add(5 * time.Minute), nil
}

func (f *memFiles) Delete(_ context.Context, id string) error {
	f.deleted = append(f.deleted, id)

	return nil
}

func newService(sources []Source) (*Service, *memStore, *memFiles, *clock) {
	store, files, c := &memStore{}, &memFiles{}, &clock{now: now}

	s := NewService(store, profiles{}, sources, files, &seqIDs{}, c,
		Settings{MinInterval: 24 * time.Hour, KeepFor: 7 * 24 * time.Hour, MaxAttempts: 2, RetryAfter: time.Minute},
		slog.New(slog.NewTextHandler(io.Discard, nil)))

	return s, store, files, c
}

func section(name, body string) Source {
	return Source{Name: name, Export: func(_ context.Context, _ string, p Profiles) ([]Section, error) {
		return []Section{{Name: name + ".json", Content: []byte(body + p.RiderID)}}, nil
	}}
}

func TestAnExportIsMadeKeptAndExpired(t *testing.T) {
	s, store, files, c := newService([]Source{section("a", "["), section("b", "[")})
	ctx := context.Background()

	created, err := s.Request(ctx, person)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.Request(ctx, person); !errors.Is(err, ErrTooSoon) {
		t.Fatalf("a second request the same day: %v", err)
	}

	if _, _, err := s.Download(ctx, person, created.ID); !errors.Is(err, ErrNotReady) {
		t.Fatalf("download before it is made: %v", err)
	}

	if made := s.MakePending(ctx); made != 1 {
		t.Fatalf("made %d", made)
	}

	reader, err := zip.NewReader(bytes.NewReader(files.stored[0]), int64(len(files.stored[0])))
	if err != nil {
		t.Fatal(err)
	}

	var names []string
	for _, f := range reader.File {
		names = append(names, f.Name)
	}

	if strings.Join(names, ",") != "README.txt,a.json,b.json" {
		t.Fatalf("files %v", names)
	}

	if r := store.ready[0]; r.Profiles.RiderID != "rider-1" || !r.ExpiresAt.Equal(now.Add(7*24*time.Hour)) {
		t.Fatalf("ready %+v", r)
	}

	if url, _, err := s.Download(ctx, person, created.ID); err != nil || url == "" {
		t.Fatalf("download: %q %v", url, err)
	}

	if _, _, err := s.Download(ctx, "22222222-2222-4222-8222-222222222222", created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("someone else's export: %v", err)
	}

	c.now = now.Add(8 * 24 * time.Hour)

	if removed := s.RemoveExpired(ctx); removed != 1 || files.deleted[0] != "media-1" {
		t.Fatalf("removed %d, deleted %v", removed, files.deleted)
	}

	if _, err := s.Request(ctx, person); err != nil {
		t.Fatalf("a request a week later: %v", err)
	}
}

func TestAFailingSourceIsRetriedThenFails(t *testing.T) {
	broken := Source{Name: "wallet", Export: func(context.Context, string, Profiles) ([]Section, error) {
		return nil, errors.New("down")
	}}
	s, store, files, _ := newService([]Source{section("a", "["), broken})
	ctx := context.Background()

	created, _ := s.Request(ctx, person)

	s.MakePending(ctx)
	if e, _ := store.Find(ctx, person, created.ID); e.Status != StatusPending || e.Attempts != 1 {
		t.Fatalf("after one failure %+v", e)
	}

	s.MakePending(ctx)
	if e, _ := store.Find(ctx, person, created.ID); e.Status != StatusFailed {
		t.Fatalf("after the last attempt %+v", e)
	}

	if len(files.stored) != 0 {
		t.Fatal("a file was stored for a failed export")
	}

	if _, err := s.Request(ctx, person); err != nil {
		t.Fatalf("a failed export does not count against the day: %v", err)
	}
}

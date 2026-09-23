package media

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"sync"
	"testing"
	"time"
)

// memoryRepository follows the same state rules as the Postgres one.
type memoryRepository struct {
	mu      sync.Mutex
	records map[string]Media
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{records: map[string]Media{}}
}

func (r *memoryRepository) Create(_ context.Context, m Media) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.records[m.ID]; exists {
		return fmt.Errorf("duplicate id %s", m.ID)
	}

	m.Status = StatusPending
	r.records[m.ID] = m

	return nil
}

func (r *memoryRepository) FindByID(_ context.Context, id string) (Media, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	found, ok := r.records[id]
	if !ok {
		return Media{}, ErrNotFound
	}

	return found, nil
}

func (r *memoryRepository) CountPending(_ context.Context, owner string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	count := 0

	for _, m := range r.records {
		if m.OwnerIdentityID == owner && m.Status == StatusPending {
			count++
		}
	}

	return count, nil
}

func (r *memoryRepository) move(id string, allowed func(Media) bool, change func(*Media)) (Media, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	found, ok := r.records[id]
	if !ok {
		return Media{}, ErrNotFound
	}

	if !allowed(found) {
		return Media{}, ErrInvalidState
	}

	change(&found)
	r.records[id] = found

	return found, nil
}

func isPending(m Media) bool { return m.Status == StatusPending }

func (r *memoryRepository) MarkReady(_ context.Context, id string, in ReadyInput) (Media, error) {
	return r.move(id, isPending, func(m *Media) {
		at := in.At
		m.Status, m.ContentType, m.SizeBytes, m.SHA256 = StatusReady, in.ContentType, in.SizeBytes, in.SHA256
		m.Width, m.Height, m.CompletedAt = in.Width, in.Height, &at
	})
}

func (r *memoryRepository) MarkRejected(_ context.Context, id, reason string, at time.Time) (Media, error) {
	return r.move(id, isPending, func(m *Media) {
		m.Status, m.RejectionReason, m.CompletedAt = StatusRejected, reason, &at
	})
}

func (r *memoryRepository) MarkDeleted(_ context.Context, id string, _ time.Time) (Media, error) {
	return r.move(id, func(m Media) bool {
		return !m.Held && (m.Status == StatusPending || m.Status == StatusReady || m.Status == StatusRejected)
	}, func(m *Media) { m.Status = StatusDeleted })
}

func (r *memoryRepository) SetHeld(_ context.Context, id string, held bool) (Media, error) {
	return r.move(id, func(m Media) bool { return m.Status == StatusReady }, func(m *Media) { m.Held = held })
}

func (r *memoryRepository) list(match func(Media) bool, limit int) []Media {
	r.mu.Lock()
	defer r.mu.Unlock()

	var out []Media

	for _, m := range r.records {
		if match(m) {
			out = append(out, m)
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })

	if len(out) > limit {
		out = out[:limit]
	}

	return out
}

func (r *memoryRepository) ListStalePending(_ context.Context, cutoff time.Time, limit int) ([]Media, error) {
	return r.list(func(m Media) bool { return m.Status == StatusPending && m.CreatedAt.Before(cutoff) }, limit), nil
}

func (r *memoryRepository) MarkExpired(_ context.Context, id string, _ time.Time) error {
	_, err := r.move(id, isPending, func(m *Media) { m.Status, m.UploadCleared = StatusExpired, true })

	return err
}

func (r *memoryRepository) ListUploadsToClear(_ context.Context, cutoff time.Time, limit int) ([]Media, error) {
	return r.list(func(m Media) bool {
		return !m.UploadCleared && m.Status != StatusPending && m.CreatedAt.Before(cutoff)
	}, limit), nil
}

func (r *memoryRepository) MarkUploadCleared(_ context.Context, id string) error {
	_, err := r.move(id, func(Media) bool { return true }, func(m *Media) { m.UploadCleared = true })

	return err
}

type storedObject struct {
	contentType string
	data        []byte
}

// memoryStore stands in for the bucket. put simulates a client PUT through a
// presigned URL.
type memoryStore struct {
	mu        sync.Mutex
	objects   map[string]storedObject
	presigned []string
	failPut   error
}

func newMemoryStore() *memoryStore {
	return &memoryStore{objects: map[string]storedObject{}}
}

func (s *memoryStore) put(key, contentType string, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.objects[key] = storedObject{contentType: contentType, data: append([]byte(nil), data...)}
}

func (s *memoryStore) object(key string) (storedObject, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	found, ok := s.objects[key]

	return found, ok
}

func (s *memoryStore) PresignPut(key, contentType string, size int64, ttl time.Duration) (string, map[string]string, time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.presigned = append(s.presigned, "PUT "+key)

	return "https://store.test/put/" + key,
		map[string]string{"Content-Type": contentType, "Content-Length": fmt.Sprint(size)},
		testNow.Add(ttl)
}

func (s *memoryStore) PresignGet(key string, ttl time.Duration) (string, time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.presigned = append(s.presigned, "GET "+key)

	return "https://store.test/get/" + key, testNow.Add(ttl)
}

func (s *memoryStore) Stat(_ context.Context, key string) (ObjectInfo, error) {
	found, ok := s.object(key)
	if !ok {
		return ObjectInfo{}, ErrNotUploaded
	}

	return ObjectInfo{Size: int64(len(found.data))}, nil
}

func (s *memoryStore) Read(_ context.Context, key string, limit int64) ([]byte, error) {
	found, ok := s.object(key)
	if !ok {
		return nil, ErrNotUploaded
	}

	if int64(len(found.data)) > limit {
		return nil, ErrObjectTooLarge
	}

	return found.data, nil
}

func (s *memoryStore) Put(_ context.Context, key, contentType string, data []byte) error {
	if s.failPut != nil {
		return s.failPut
	}

	s.put(key, contentType, data)

	return nil
}

func (s *memoryStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.objects, key)

	return nil
}

var testNow = time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)

type manualClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *manualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.now
}

func (c *manualClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.now = c.now.Add(d)
}

type sequenceIDs struct {
	mu   sync.Mutex
	next int
}

func (g *sequenceIDs) NewID() string {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.next++

	return fmt.Sprintf("00000000-0000-4000-8000-%012d", g.next)
}

const (
	ownerA = "11111111-1111-4111-8111-111111111111"
	ownerB = "22222222-2222-4222-8222-222222222222"
)

var testSettings = Settings{
	UploadURLTTL:             10 * time.Minute,
	DownloadURLTTL:           5 * time.Minute,
	PendingTTL:               30 * time.Minute,
	MaxPendingPerOwner:       3,
	MaxConcurrentInspections: 2,
}

type fixture struct {
	service *Service
	repo    *memoryRepository
	store   *memoryStore
	clock   *manualClock
}

func newFixture(t *testing.T) fixture {
	t.Helper()

	f := fixture{
		repo:  newMemoryRepository(),
		store: newMemoryStore(),
		clock: &manualClock{now: testNow},
	}

	f.service = NewService(f.repo, f.store, &sequenceIDs{}, f.clock, testSettings,
		slog.New(slog.NewTextHandler(io.Discard, nil)))

	return f
}

// upload reserves a file for ownerA and sends data to the upload URL.
func (f fixture) upload(t *testing.T, purpose Purpose, contentType string, data []byte) Media {
	t.Helper()

	created, _, err := f.service.CreateUpload(context.Background(), CreateUploadInput{
		OwnerIdentityID: ownerA,
		Purpose:         purpose,
		ContentType:     contentType,
		SizeBytes:       int64(len(data)),
	})
	if err != nil {
		t.Fatalf("CreateUpload: %v", err)
	}

	f.store.put(created.UploadKey(), contentType, data)

	return created
}

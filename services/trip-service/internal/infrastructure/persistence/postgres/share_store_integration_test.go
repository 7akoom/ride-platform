package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/share"
	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

func hashFor(token string) []byte {
	sum := sha256.Sum256([]byte(token))

	return sum[:]
}

func TestLinksAreCountedFoundAndStopped(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	store := NewShareStore(pool)
	now := time.Now().UTC().Truncate(time.Microsecond)

	shared, err := NewTripRepository(pool).Create(ctx, trip.CreateInput{
		ID: uuid.NewString(), RiderID: uuid.NewString(),
		Pickup:  trip.Coordinates{Latitude: 36.1, Longitude: 44.1},
		Dropoff: trip.Coordinates{Latitude: 36.2, Longitude: 44.2},
	})
	if err != nil {
		t.Fatal(err)
	}

	link := func(token string, created time.Time) share.Link {
		return share.Link{TripID: shared.ID, TokenHash: hashFor(token), CreatedAt: created, ExpiresAt: created.Add(time.Hour)}
	}

	// Three riders' taps at once, two places: two links, never three.
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		made    int
		refused int
	)

	for i, token := range []string{"a", "b", "c"} {
		wg.Add(1)

		go func() {
			defer wg.Done()

			_, err := store.Create(ctx, link(token, now.Add(time.Duration(i)*time.Millisecond)), 2)

			mu.Lock()
			defer mu.Unlock()

			switch {
			case err == nil:
				made++
			case errors.Is(err, share.ErrTooManyLinks):
				refused++
			default:
				t.Error(err)
			}
		}()
	}

	wg.Wait()

	if made != 2 || refused != 1 {
		t.Fatalf("made %d, refused %d", made, refused)
	}

	var liveToken string

	for _, token := range []string{"a", "b", "c"} {
		if found, err := store.FindLive(ctx, hashFor(token), now); err == nil {
			if found.TripID != shared.ID {
				t.Fatalf("found %+v", found)
			}

			liveToken = token
		}
	}

	if liveToken == "" {
		t.Fatal("no live link found")
	}

	if _, err := store.FindLive(ctx, hashFor(liveToken), now.Add(2*time.Hour)); !errors.Is(err, share.ErrNotFound) {
		t.Fatalf("an expired link: %v", err)
	}

	if n, err := store.RevokeAll(ctx, shared.ID, now); err != nil || n != 2 {
		t.Fatalf("revoked %d %v", n, err)
	}

	if _, err := store.FindLive(ctx, hashFor(liveToken), now); !errors.Is(err, share.ErrNotFound) {
		t.Fatalf("a stopped link: %v", err)
	}

	// Stopped links free their places.
	if _, err := store.Create(ctx, link("d", now), 2); err != nil {
		t.Fatalf("after stopping: %v", err)
	}

	// A token is one link, ever.
	if _, err := store.Create(ctx, link("d", now), 5); err == nil {
		t.Fatal("the same token twice")
	}
}

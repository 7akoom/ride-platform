package valkey

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/7akoom/ride-platform/services/location-service/internal/application/location"
	valkeygo "github.com/valkey-io/valkey-go"
)

// Design note: Redis/Valkey sorted sets (used by GEOADD) don't support a
// per-member TTL, only a TTL on the whole key. Since entities come and go
// independently, we can't just put a TTL on the geo set itself. Instead we
// keep a small companion "meta" string key per entity with a TTL — that key
// expiring is what "this entity went stale/offline" means. GEOSEARCH alone
// could still return a member whose meta already expired (Valkey doesn't
// remove sorted-set members on their own), so FindNearby double-checks
// freshness against the meta keys before returning results. This mirrors
// the "periodic cleanup for stale drivers" pattern used in production
// systems, just done lazily at query time instead of via a background job.
type locationMeta struct {
	Latitude  float64   `json:"lat"`
	Longitude float64   `json:"lng"`
	UpdatedAt time.Time `json:"updated_at"`
}

type LocationStore struct {
	client valkeygo.Client
}

func NewLocationStore(client valkeygo.Client) *LocationStore {
	if client == nil {
		panic("Valkey client is required")
	}

	return &LocationStore{client: client}
}

func geoKey(entityType location.EntityType) string {
	return "geo:" + string(entityType)
}

func metaKey(entityType location.EntityType, entityID string) string {
	return "loc:" + string(entityType) + ":" + entityID
}

func (s *LocationStore) Update(
	ctx context.Context,
	input location.UpdateInput,
) (time.Time, error) {
	now := time.Now().UTC()

	meta := locationMeta{
		Latitude:  input.Coordinates.Latitude,
		Longitude: input.Coordinates.Longitude,
		UpdatedAt: now,
	}

	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return time.Time{}, fmt.Errorf("marshal location meta: %w", err)
	}

	geoCmd := s.client.B().
		Geoadd().
		Key(geoKey(input.EntityType)).
		LongitudeLatitudeMember().
		LongitudeLatitudeMember(
			input.Coordinates.Longitude,
			input.Coordinates.Latitude,
			input.EntityID,
		).
		Build()

	setCmd := s.client.B().
		Set().
		Key(metaKey(input.EntityType, input.EntityID)).
		Value(string(metaJSON)).
		Ex(input.TTL).
		Build()

	for _, resp := range s.client.DoMulti(ctx, geoCmd, setCmd) {
		if err := resp.Error(); err != nil {
			return time.Time{}, fmt.Errorf("write location to Valkey: %w", err)
		}
	}

	return now, nil
}

func (s *LocationStore) Get(
	ctx context.Context,
	entityType location.EntityType,
	entityID string,
) (location.Location, error) {
	raw, err := s.client.Do(
		ctx,
		s.client.B().Get().Key(metaKey(entityType, entityID)).Build(),
	).ToString()
	if err != nil {
		if valkeygo.IsValkeyNil(err) {
			return location.Location{}, location.ErrLocationNotFound
		}

		return location.Location{}, fmt.Errorf("read location meta: %w", err)
	}

	var meta locationMeta

	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		return location.Location{}, fmt.Errorf("unmarshal location meta: %w", err)
	}

	return location.Location{
		EntityType: entityType,
		EntityID:   entityID,
		Coordinates: location.Coordinates{
			Latitude:  meta.Latitude,
			Longitude: meta.Longitude,
		},
		UpdatedAt: meta.UpdatedAt,
	}, nil
}

func (s *LocationStore) FindNearby(
	ctx context.Context,
	input location.NearbySearchInput,
) ([]location.NearbyEntity, error) {
	searchCmd := s.client.B().
		Geosearch().
		Key(geoKey(input.EntityType)).
		Fromlonlat(input.Coordinates.Longitude, input.Coordinates.Latitude).
		Byradius(input.RadiusMeters).
		M().
		Asc().
		Count(int64(input.Limit)).
		Withcoord().
		Withdist().
		Build()

	candidates, err := s.client.Do(ctx, searchCmd).AsGeosearch()
	if err != nil {
		return nil, fmt.Errorf("geosearch: %w", err)
	}

	if len(candidates) == 0 {
		return []location.NearbyEntity{}, nil
	}

	// Filter out candidates whose meta key already expired (stale/offline)
	// — see the design note on LocationStore for why this check exists.
	metaKeys := make([]string, len(candidates))
	for i, candidate := range candidates {
		metaKeys[i] = metaKey(input.EntityType, candidate.Name)
	}

	freshness, err := s.client.Do(
		ctx,
		s.client.B().Mget().Key(metaKeys...).Build(),
	).ToArray()
	if err != nil {
		return nil, fmt.Errorf("check location freshness: %w", err)
	}

	results := make([]location.NearbyEntity, 0, len(candidates))

	for i, candidate := range candidates {
		if i >= len(freshness) || freshness[i].IsNil() {
			continue
		}

		results = append(results, location.NearbyEntity{
			EntityID: candidate.Name,
			Coordinates: location.Coordinates{
				Latitude:  candidate.Latitude,
				Longitude: candidate.Longitude,
			},
			DistanceMeters: candidate.Dist,
		})
	}

	return results, nil
}

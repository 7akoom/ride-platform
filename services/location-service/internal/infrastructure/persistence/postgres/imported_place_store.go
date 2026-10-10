package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/7akoom/ride-platform/services/location-service/internal/application/maps"
)

// ImportedPlaceStore searches the places imported from an open places dataset
// (scripts/tools/import-places fills imported_places; this store only reads).
type ImportedPlaceStore struct {
	pool *pgxpool.Pool
}

var _ maps.ImportedSearcher = (*ImportedPlaceStore)(nil)

func NewImportedPlaceStore(pool *pgxpool.Pool) *ImportedPlaceStore {
	if pool == nil {
		panic("PostgreSQL pool is required")
	}

	return &ImportedPlaceStore{pool: pool}
}

// SearchImported matches the query inside a name or close to a word of it
// (trigram word similarity), both compared through place_search_text. Ranked:
// a name holding the query first, then closer matches (in steps of 0.1), the
// kind's weight, nearness (3 km steps) and the dataset's confidence.
func (s *ImportedPlaceStore) SearchImported(
	ctx context.Context,
	query string,
	near *maps.Coordinates,
	limit int,
) ([]maps.Place, error) {
	distance := "0::float8"
	args := []any{query, limit}

	if near != nil {
		args = append(args, fmt.Sprintf("SRID=4326;POINT(%g %g)", near.Longitude, near.Latitude))
		distance = "ST_Distance(p.location, ST_GeogFromText($3))"
	}

	rows, err := s.pool.Query(ctx, `
		WITH q AS (SELECT place_search_text($1) AS text)
		SELECT p.id, p.name, p.kind, p.address,
		       ST_Y(p.location::geometry), ST_X(p.location::geometry)
		FROM imported_places p, q
		WHERE q.text <> ''
		  AND (strpos(p.search_text, q.text) > 0 OR q.text <% p.search_text)
		ORDER BY (strpos(p.search_text, q.text) > 0) DESC,
		         round(word_similarity(q.text, p.search_text)::numeric, 1) DESC,
		         p.weight DESC,
		         floor(`+distance+` / 3000),
		         p.confidence DESC,
		         p.id
		LIMIT $2`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("search imported places: %w", err)
	}
	defer rows.Close()

	places := make([]maps.Place, 0)

	for rows.Next() {
		var (
			id, name, kind, address string
			latitude, longitude     float64
		)

		if err := rows.Scan(&id, &name, &kind, &address, &latitude, &longitude); err != nil {
			return nil, fmt.Errorf("search imported places: %w", err)
		}

		places = append(places, importedPlace(id, name, kind, address, maps.Coordinates{
			Latitude: latitude, Longitude: longitude,
		}))
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search imported places: %w", err)
	}

	return places, nil
}

// importedPlace shows an imported place the way map results are shown.
func importedPlace(id, name, kind, address string, at maps.Coordinates) maps.Place {
	display := name
	if address != "" {
		display = strings.Join([]string{name, address}, ", ")
	}

	return maps.Place{
		ID:          "imported/" + id,
		Name:        name,
		DisplayName: display,
		Category:    kind,
		Type:        "imported",
		Coordinates: at,
		Address:     map[string]string{},
	}
}

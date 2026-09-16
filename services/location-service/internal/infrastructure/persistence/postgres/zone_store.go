package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/7akoom/ride-platform/services/location-service/internal/application/zone"
)

type ZoneStore struct {
	pool *pgxpool.Pool
}

func NewZoneStore(
	pool *pgxpool.Pool,
) *ZoneStore {
	if pool == nil {
		panic("PostgreSQL pool is required")
	}

	return &ZoneStore{pool: pool}
}

const zoneColumns = `id, city, name, ST_AsGeoJSON(boundary::geometry), active, created_at, updated_at`

// rowScanner is satisfied by both pgx.Row (QueryRow) and pgx.Rows
// (Query, one row at a time via Next), so scanZoneRow works for both a
// single lookup and a list scan.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanZoneRow(row rowScanner) (zone.Zone, error) {
	var (
		id, city, name, boundaryGeoJSON string
		active                          bool
		createdAt, updatedAt            time.Time
	)

	if err := row.Scan(
		&id, &city, &name, &boundaryGeoJSON, &active, &createdAt, &updatedAt,
	); err != nil {
		return zone.Zone{}, fmt.Errorf("scan zone row: %w", err)
	}

	boundary, err := boundaryFromGeoJSON(boundaryGeoJSON)
	if err != nil {
		return zone.Zone{}, err
	}

	return zone.Zone{
		ID:        id,
		City:      city,
		Name:      name,
		Boundary:  boundary,
		Active:    active,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}, nil
}

// boundaryToWKT renders a boundary as a closed WKT polygon ring
// (PostGIS requires the first vertex repeated at the end; the domain
// type deliberately does not store it that way, so we close it here).
// WKT coordinate order is (longitude latitude) — the opposite of how
// this codebase's Coordinates fields read.
func boundaryToWKT(boundary []zone.Coordinates) string {
	points := make([]string, 0, len(boundary)+1)

	for _, point := range boundary {
		points = append(points, fmt.Sprintf("%g %g", point.Longitude, point.Latitude))
	}

	first := boundary[0]
	points = append(points, fmt.Sprintf("%g %g", first.Longitude, first.Latitude))

	return fmt.Sprintf("SRID=4326;POLYGON((%s))", strings.Join(points, ", "))
}

func pointToWKT(point zone.Coordinates) string {
	return fmt.Sprintf("SRID=4326;POINT(%g %g)", point.Longitude, point.Latitude)
}

type geoJSONPolygon struct {
	Coordinates [][][2]float64 `json:"coordinates"`
}

func boundaryFromGeoJSON(raw string) ([]zone.Coordinates, error) {
	var parsed geoJSONPolygon

	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, fmt.Errorf("parse zone boundary: %w", err)
	}

	if len(parsed.Coordinates) == 0 {
		return nil, errors.New("zone boundary has no rings")
	}

	ring := parsed.Coordinates[0]

	// GeoJSON repeats the first vertex at the end to close the ring;
	// the domain type does not, so drop it.
	if len(ring) > 1 {
		ring = ring[:len(ring)-1]
	}

	boundary := make([]zone.Coordinates, len(ring))

	for i, vertex := range ring {
		boundary[i] = zone.Coordinates{
			Longitude: vertex[0],
			Latitude:  vertex[1],
		}
	}

	return boundary, nil
}

func (s *ZoneStore) Create(
	ctx context.Context,
	input zone.CreateInput,
) (zone.Zone, error) {
	query := fmt.Sprintf(`
		INSERT INTO zones (id, city, name, boundary, active)
		VALUES ($1, $2, $3, ST_GeogFromText($4), TRUE)
		RETURNING %s
	`, zoneColumns)

	row := s.pool.QueryRow(
		ctx, query,
		input.ID, input.City, input.Name, boundaryToWKT(input.Boundary),
	)

	created, err := scanZoneRow(row)
	if err != nil {
		return zone.Zone{}, fmt.Errorf("insert zone: %w", err)
	}

	return created, nil
}

func (s *ZoneStore) Update(
	ctx context.Context,
	input zone.UpdateInput,
) (zone.Zone, error) {
	query := fmt.Sprintf(`
		UPDATE zones
		SET name = $2, boundary = ST_GeogFromText($3), updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
		RETURNING %s
	`, zoneColumns)

	row := s.pool.QueryRow(
		ctx, query,
		input.ZoneID, input.Name, boundaryToWKT(input.Boundary),
	)

	updated, err := scanZoneRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return zone.Zone{}, zone.ErrZoneNotFound
	}
	if err != nil {
		return zone.Zone{}, fmt.Errorf("update zone: %w", err)
	}

	return updated, nil
}

func (s *ZoneStore) SetActive(
	ctx context.Context,
	zoneID string,
	active bool,
) (zone.Zone, error) {
	query := fmt.Sprintf(`
		UPDATE zones
		SET active = $2, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
		RETURNING %s
	`, zoneColumns)

	row := s.pool.QueryRow(ctx, query, zoneID, active)

	updated, err := scanZoneRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return zone.Zone{}, zone.ErrZoneNotFound
	}
	if err != nil {
		return zone.Zone{}, fmt.Errorf("set zone active: %w", err)
	}

	return updated, nil
}

func (s *ZoneStore) Get(
	ctx context.Context,
	zoneID string,
) (zone.Zone, error) {
	query := fmt.Sprintf(`SELECT %s FROM zones WHERE id = $1`, zoneColumns)

	row := s.pool.QueryRow(ctx, query, zoneID)

	found, err := scanZoneRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return zone.Zone{}, zone.ErrZoneNotFound
	}
	if err != nil {
		return zone.Zone{}, fmt.Errorf("get zone: %w", err)
	}

	return found, nil
}

func (s *ZoneStore) List(
	ctx context.Context,
	city string,
) ([]zone.Zone, error) {
	var (
		rows pgx.Rows
		err  error
	)

	if city == "" {
		query := fmt.Sprintf(`SELECT %s FROM zones ORDER BY city, name`, zoneColumns)
		rows, err = s.pool.Query(ctx, query)
	} else {
		query := fmt.Sprintf(`SELECT %s FROM zones WHERE city = $1 ORDER BY name`, zoneColumns)
		rows, err = s.pool.Query(ctx, query, city)
	}

	if err != nil {
		return nil, fmt.Errorf("list zones: %w", err)
	}
	defer rows.Close()

	results := make([]zone.Zone, 0)

	for rows.Next() {
		found, err := scanZoneRow(rows)
		if err != nil {
			return nil, err
		}

		results = append(results, found)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list zones: %w", err)
	}

	return results, nil
}

func (s *ZoneStore) FindContaining(
	ctx context.Context,
	point zone.Coordinates,
) (zone.Zone, bool, error) {
	query := fmt.Sprintf(`
		SELECT %s
		FROM zones
		WHERE active = TRUE
		  AND ST_Covers(boundary, ST_GeogFromText($1))
		LIMIT 1
	`, zoneColumns)

	row := s.pool.QueryRow(ctx, query, pointToWKT(point))

	found, err := scanZoneRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return zone.Zone{}, false, nil
	}
	if err != nil {
		return zone.Zone{}, false, fmt.Errorf("find containing zone: %w", err)
	}

	return found, true, nil
}

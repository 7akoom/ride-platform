package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/7akoom/ride-platform/services/location-service/internal/application/place"
)

type PlaceStore struct {
	pool *pgxpool.Pool
}

var _ place.Repository = (*PlaceStore)(nil)

func NewPlaceStore(pool *pgxpool.Pool) *PlaceStore {
	if pool == nil {
		panic("PostgreSQL pool is required")
	}

	return &PlaceStore{pool: pool}
}

// placeColumns reads a place with its city's names; every query selects FROM
// curated_places p JOIN cities c ON c.id = p.city_id.
const placeColumns = `p.id, p.city_id, c.name, c.names, p.category, p.name, p.names, p.address,
	ST_Y(p.location::geometry), ST_X(p.location::geometry), p.priority, p.active, p.created_at, p.updated_at`

const placeFrom = `curated_places p JOIN cities c ON c.id = p.city_id`

func scanPlace(row rowScanner, extra ...any) (place.Place, error) {
	var (
		found               place.Place
		category            string
		cityNames, rawNames []byte
	)

	dest := []any{
		&found.ID, &found.CityID, &found.CityName, &cityNames, &category, &found.Name, &rawNames, &found.Address,
		&found.Coordinates.Latitude, &found.Coordinates.Longitude, &found.Priority, &found.Active,
		&found.CreatedAt, &found.UpdatedAt,
	}

	if err := row.Scan(append(dest, extra...)...); err != nil {
		return place.Place{}, fmt.Errorf("scan place: %w", err)
	}

	var err error

	if found.CityNames, err = decodeNames(cityNames); err != nil {
		return place.Place{}, err
	}

	if found.Names, err = decodeNames(rawNames); err != nil {
		return place.Place{}, err
	}

	found.Category = place.Category(category)

	return found, nil
}

func pointWKT(c place.Coordinates) string {
	return fmt.Sprintf("SRID=4326;POINT(%g %g)", c.Longitude, c.Latitude)
}

func (s *PlaceStore) Create(ctx context.Context, id string, cityID string, details place.Details) (place.Place, error) {
	names, err := encodeNames(details.Names)
	if err != nil {
		return place.Place{}, err
	}

	created, err := scanPlace(s.pool.QueryRow(ctx, `
		WITH p AS (
			INSERT INTO curated_places (id, city_id, category, name, names, address, location, priority)
			VALUES ($1, $2, $3, $4, $5, $6, ST_GeogFromText($7), $8)
			RETURNING *
		)
		SELECT `+placeColumns+` FROM p JOIN cities c ON c.id = p.city_id`,
		id, cityID, string(details.Category), details.Name, names, details.Address,
		pointWKT(details.Coordinates), details.Priority,
	))
	if isForeignKeyViolation(err) {
		return place.Place{}, place.ErrCityNotFound
	}

	if err != nil {
		return place.Place{}, fmt.Errorf("insert place: %w", err)
	}

	return created, nil
}

func (s *PlaceStore) Update(ctx context.Context, id string, details place.Details) (place.Place, error) {
	names, err := encodeNames(details.Names)
	if err != nil {
		return place.Place{}, err
	}

	updated, err := scanPlace(s.pool.QueryRow(ctx, `
		WITH p AS (
			UPDATE curated_places
			SET category = $2, name = $3, names = $4, address = $5, location = ST_GeogFromText($6),
			    priority = $7, updated_at = CURRENT_TIMESTAMP
			WHERE id = $1
			RETURNING *
		)
		SELECT `+placeColumns+` FROM p JOIN cities c ON c.id = p.city_id`,
		id, string(details.Category), details.Name, names, details.Address,
		pointWKT(details.Coordinates), details.Priority,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return place.Place{}, place.ErrPlaceNotFound
	}

	if err != nil {
		return place.Place{}, fmt.Errorf("update place: %w", err)
	}

	return updated, nil
}

func (s *PlaceStore) SetActive(ctx context.Context, id string, active bool) (place.Place, error) {
	updated, err := scanPlace(s.pool.QueryRow(ctx, `
		WITH p AS (
			UPDATE curated_places SET active = $2, updated_at = CURRENT_TIMESTAMP
			WHERE id = $1
			RETURNING *
		)
		SELECT `+placeColumns+` FROM p JOIN cities c ON c.id = p.city_id`,
		id, active,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return place.Place{}, place.ErrPlaceNotFound
	}

	if err != nil {
		return place.Place{}, fmt.Errorf("set place active: %w", err)
	}

	return updated, nil
}

// Get returns a place; one in an inactive city reads as inactive.
func (s *PlaceStore) Get(ctx context.Context, id string) (place.Place, error) {
	var cityActive bool

	found, err := scanPlace(s.pool.QueryRow(ctx,
		`SELECT `+placeColumns+`, c.active FROM `+placeFrom+` WHERE p.id = $1`, id), &cityActive)
	if errors.Is(err, pgx.ErrNoRows) {
		return place.Place{}, place.ErrPlaceNotFound
	}

	if err != nil {
		return place.Place{}, fmt.Errorf("get place: %w", err)
	}

	found.Active = found.Active && cityActive

	return found, nil
}

func (s *PlaceStore) List(ctx context.Context, filter place.Filter) ([]place.Place, error) {
	conditions := []string{"TRUE"}
	args := []any{}

	arg := func(value any) string {
		args = append(args, value)

		return fmt.Sprintf("$%d", len(args))
	}

	if !filter.IncludeInactive {
		conditions = append(conditions, "p.active AND c.active")
	}

	if filter.CityID != "" {
		conditions = append(conditions, "p.city_id = "+arg(filter.CityID))
	}

	if filter.Category != "" {
		conditions = append(conditions, "p.category = "+arg(string(filter.Category)))
	}

	order := "p.priority DESC, lower(p.name), p.id"
	if filter.Near != nil {
		order = "p.priority DESC, ST_Distance(p.location, ST_GeogFromText(" +
			arg(pointWKT(*filter.Near)) + ")), p.id"
	}

	query := `SELECT ` + placeColumns + ` FROM ` + placeFrom +
		` WHERE ` + strings.Join(conditions, " AND ") +
		` ORDER BY ` + order +
		` OFFSET ` + arg(filter.Offset) + ` LIMIT ` + arg(filter.Limit+1)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list places: %w", err)
	}
	defer rows.Close()

	places := make([]place.Place, 0)

	for rows.Next() {
		found, err := scanPlace(rows)
		if err != nil {
			return nil, err
		}

		places = append(places, found)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list places: %w", err)
	}

	return places, nil
}

// Search matches the query inside any name (or close to it, by trigrams),
// in active places of active cities. A name containing the query ranks above
// a merely similar one; then closer places (by 2 km steps) and higher
// priority come first.
func (s *PlaceStore) Search(ctx context.Context, query string, near *place.Coordinates, limit int) ([]place.Match, error) {
	lowered := strings.ToLower(query)
	pattern := "%" + escapeLike(lowered) + "%"

	distance := "0::float8"
	args := []any{lowered, pattern, limit}

	if near != nil {
		args = append(args, pointWKT(*near))
		distance = "ST_Distance(p.location, ST_GeogFromText($4))"
	}

	rows, err := s.pool.Query(ctx, `
		SELECT `+placeColumns+`, `+distance+` AS distance
		FROM `+placeFrom+`
		WHERE p.active AND c.active
		  AND (p.search_text LIKE $2 ESCAPE '\' OR p.search_text % $1)
		ORDER BY (p.search_text LIKE $2 ESCAPE '\') DESC,
		         floor(`+distance+` / 2000),
		         p.priority DESC,
		         similarity(p.search_text, $1) DESC,
		         p.id
		LIMIT $3`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("search places: %w", err)
	}
	defer rows.Close()

	matches := make([]place.Match, 0)

	for rows.Next() {
		var match place.Match

		found, err := scanPlace(rows, &match.DistanceMeters)
		if err != nil {
			return nil, err
		}

		match.Place = found
		matches = append(matches, match)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search places: %w", err)
	}

	return matches, nil
}

func escapeLike(value string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(value)
}

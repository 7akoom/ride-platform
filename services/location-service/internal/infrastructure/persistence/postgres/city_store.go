package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/7akoom/ride-platform/services/location-service/internal/application/city"
)

type CityStore struct {
	pool *pgxpool.Pool
}

var _ city.Repository = (*CityStore)(nil)

func NewCityStore(pool *pgxpool.Pool) *CityStore {
	if pool == nil {
		panic("PostgreSQL pool is required")
	}

	return &CityStore{pool: pool}
}

const cityColumns = `id, name, names, time_zone, center_latitude, center_longitude, active, created_at, updated_at`

func scanCity(row rowScanner) (city.City, error) {
	var (
		found    city.City
		rawNames []byte
	)

	if err := row.Scan(
		&found.ID, &found.Name, &rawNames, &found.TimeZone,
		&found.Center.Latitude, &found.Center.Longitude,
		&found.Active, &found.CreatedAt, &found.UpdatedAt,
	); err != nil {
		return city.City{}, fmt.Errorf("scan city: %w", err)
	}

	names, err := decodeNames(rawNames)
	if err != nil {
		return city.City{}, err
	}

	found.Names = names

	return found, nil
}

func (s *CityStore) Create(ctx context.Context, id string, details city.Details) (city.City, error) {
	names, err := encodeNames(details.Names)
	if err != nil {
		return city.City{}, err
	}

	created, err := scanCity(s.pool.QueryRow(ctx, `
		INSERT INTO cities (id, name, names, time_zone, center_latitude, center_longitude)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING `+cityColumns,
		id, details.Name, names, details.TimeZone, details.Center.Latitude, details.Center.Longitude,
	))
	if isUniqueViolation(err) {
		return city.City{}, city.ErrCityNameTaken
	}

	if err != nil {
		return city.City{}, fmt.Errorf("insert city: %w", err)
	}

	return created, nil
}

func (s *CityStore) Update(ctx context.Context, id string, details city.Details) (city.City, error) {
	names, err := encodeNames(details.Names)
	if err != nil {
		return city.City{}, err
	}

	updated, err := scanCity(s.pool.QueryRow(ctx, `
		UPDATE cities
		SET name = $2, names = $3, time_zone = $4, center_latitude = $5, center_longitude = $6,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
		RETURNING `+cityColumns,
		id, details.Name, names, details.TimeZone, details.Center.Latitude, details.Center.Longitude,
	))

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return city.City{}, city.ErrCityNotFound
	case isUniqueViolation(err):
		return city.City{}, city.ErrCityNameTaken
	case err != nil:
		return city.City{}, fmt.Errorf("update city: %w", err)
	}

	return updated, nil
}

func (s *CityStore) SetActive(ctx context.Context, id string, active bool) (city.City, error) {
	updated, err := scanCity(s.pool.QueryRow(ctx, `
		UPDATE cities SET active = $2, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
		RETURNING `+cityColumns,
		id, active,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return city.City{}, city.ErrCityNotFound
	}

	if err != nil {
		return city.City{}, fmt.Errorf("set city active: %w", err)
	}

	return updated, nil
}

func (s *CityStore) Get(ctx context.Context, id string) (city.City, error) {
	found, err := scanCity(s.pool.QueryRow(ctx, `SELECT `+cityColumns+` FROM cities WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return city.City{}, city.ErrCityNotFound
	}

	if err != nil {
		return city.City{}, fmt.Errorf("get city: %w", err)
	}

	return found, nil
}

func (s *CityStore) List(ctx context.Context, includeInactive bool) ([]city.City, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+cityColumns+` FROM cities
		WHERE active OR $1
		ORDER BY lower(name)`,
		includeInactive,
	)
	if err != nil {
		return nil, fmt.Errorf("list cities: %w", err)
	}
	defer rows.Close()

	cities := make([]city.City, 0)

	for rows.Next() {
		found, err := scanCity(rows)
		if err != nil {
			return nil, err
		}

		cities = append(cities, found)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list cities: %w", err)
	}

	return cities, nil
}

func encodeNames(names map[string]string) ([]byte, error) {
	if names == nil {
		names = map[string]string{}
	}

	raw, err := json.Marshal(names)
	if err != nil {
		return nil, fmt.Errorf("encode names: %w", err)
	}

	return raw, nil
}

func decodeNames(raw []byte) (map[string]string, error) {
	names := map[string]string{}

	if len(raw) == 0 {
		return names, nil
	}

	if err := json.Unmarshal(raw, &names); err != nil {
		return nil, fmt.Errorf("decode names: %w", err)
	}

	return names, nil
}

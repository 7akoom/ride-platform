package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	"github.com/jackc/pgx/v5/pgxpool"
)

type IdentityRepository struct {
	pool *pgxpool.Pool
}

var _ auth.IdentityRepository = (*IdentityRepository)(nil)

func NewIdentityRepository(
	pool *pgxpool.Pool,
) *IdentityRepository {
	if pool == nil {
		panic("PostgreSQL pool is required")
	}

	return &IdentityRepository{
		pool: pool,
	}
}

func (r *IdentityRepository) FindOrCreateByPhoneNumber(
	ctx context.Context,
	phoneNumber string,
) (auth.Identity, error) {
	if strings.TrimSpace(phoneNumber) == "" {
		return auth.Identity{}, errors.New(
			"phone number cannot be blank",
		)
	}
	const insertQuery = `
		INSERT INTO identities (
			phone_number
		)
		VALUES ($1)
		ON CONFLICT (phone_number) DO NOTHING
	`

	if _, err := r.pool.Exec(
		ctx,
		insertQuery,
		phoneNumber,
	); err != nil {
		return auth.Identity{}, fmt.Errorf(
			"insert identity: %w",
			err,
		)
	}

	const selectQuery = `
		SELECT
			id::text,
			phone_number,
			status
		FROM identities
		WHERE phone_number = $1
	`

	var identity auth.Identity
	var status string

	if err := r.pool.QueryRow(
		ctx,
		selectQuery,
		phoneNumber,
	).Scan(
		&identity.ID,
		&identity.PhoneNumber,
		&status,
	); err != nil {
		return auth.Identity{}, fmt.Errorf(
			"query identity: %w",
			err,
		)
	}

	identity.IsActive = status == "active"

	return identity, nil
}

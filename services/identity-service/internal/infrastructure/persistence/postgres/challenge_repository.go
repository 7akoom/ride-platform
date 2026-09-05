package postgres

import (
	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ChallengeRepository struct {
	pool *pgxpool.Pool
}

var _ auth.ChallengeRepository = (*ChallengeRepository)(nil)

func NewChallengeRepository(
	pool *pgxpool.Pool,
) *ChallengeRepository {
	if pool == nil {
		panic("PostgreSQL pool is required")
	}

	return &ChallengeRepository{
		pool: pool,
	}
}

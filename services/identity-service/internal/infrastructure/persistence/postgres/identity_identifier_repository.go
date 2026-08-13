package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type IdentityIdentifierRepository struct {
	pool *pgxpool.Pool
}

var _ auth.IdentityIdentifierRepository = (*IdentityIdentifierRepository)(nil)

func NewIdentityIdentifierRepository(
	pool *pgxpool.Pool,
) *IdentityIdentifierRepository {
	if pool == nil {
		panic("PostgreSQL pool is required")
	}

	return &IdentityIdentifierRepository{
		pool: pool,
	}
}

func (r *IdentityIdentifierRepository) FindIdentityByIdentifier(
	ctx context.Context,
	identifier auth.Identifier,
) (auth.Identity, bool, error) {
	normalized, err := auth.NewIdentifier(
		identifier.Type,
		identifier.Value,
	)
	if err != nil {
		return auth.Identity{}, false, err
	}

	const query = `
		SELECT
			i.id::text,
			i.status
		FROM identity_identifiers ii
		INNER JOIN identities i
			ON i.id = ii.identity_id
		WHERE ii.identifier_type = $1
		  AND ii.normalized_value = $2
	`

	var identity auth.Identity
	var status string

	err = r.pool.QueryRow(
		ctx,
		query,
		string(normalized.Type),
		normalized.Value,
	).Scan(
		&identity.ID,
		&status,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return auth.Identity{}, false, nil
	}

	if err != nil {
		return auth.Identity{}, false, fmt.Errorf(
			"query identity by identifier: %w",
			err,
		)
	}

	identity.IsActive = status == "active"

	return identity, true, nil
}

func (r *IdentityIdentifierRepository) CreateIdentityWithIdentifier(
	ctx context.Context,
	identifier auth.Identifier,
	verifiedAt time.Time,
) (auth.Identity, error) {
	normalized, err := auth.NewIdentifier(
		identifier.Type,
		identifier.Value,
	)
	if err != nil {
		return auth.Identity{}, err
	}

	if verifiedAt.IsZero() {
		return auth.Identity{}, errors.New(
			"identifier verification time cannot be zero",
		)
	}

	verifiedAt = verifiedAt.UTC()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return auth.Identity{}, fmt.Errorf(
			"begin identity creation transaction: %w",
			err,
		)
	}
	defer tx.Rollback(ctx)

	if err := lockIdentityIdentifier(
		ctx,
		tx,
		normalized,
	); err != nil {
		return auth.Identity{}, err
	}

	existingIdentity, found, err := findIdentityByIdentifierTx(
		ctx,
		tx,
		normalized,
	)
	if err != nil {
		return auth.Identity{}, err
	}

	if found {
		if err := tx.Commit(ctx); err != nil {
			return auth.Identity{}, fmt.Errorf(
				"commit existing identity lookup: %w",
				err,
			)
		}

		return existingIdentity, nil
	}

	const insertIdentityQuery = `
		INSERT INTO identities
			DEFAULT VALUES
		RETURNING
			id::text,
			status
	`

	var identity auth.Identity
	var status string

	if err := tx.QueryRow(
		ctx,
		insertIdentityQuery,
	).Scan(
		&identity.ID,
		&status,
	); err != nil {
		return auth.Identity{}, fmt.Errorf(
			"insert identity: %w",
			err,
		)
	}

	const insertIdentifierQuery = `
		INSERT INTO identity_identifiers (
			identity_id,
			identifier_type,
			normalized_value,
			verified_at
		)
		VALUES ($1, $2, $3, $4)
	`

	if _, err := tx.Exec(
		ctx,
		insertIdentifierQuery,
		identity.ID,
		string(normalized.Type),
		normalized.Value,
		verifiedAt,
	); err != nil {
		if isIdentifierOwnershipConflict(err) {
			return auth.Identity{},
				auth.ErrIdentifierAlreadyLinked
		}

		return auth.Identity{}, fmt.Errorf(
			"insert identity identifier: %w",
			err,
		)
	}

	if err := tx.Commit(ctx); err != nil {
		return auth.Identity{}, fmt.Errorf(
			"commit identity creation transaction: %w",
			err,
		)
	}

	identity.IsActive = status == "active"

	return identity, nil
}

func (r *IdentityIdentifierRepository) LinkIdentifier(
	ctx context.Context,
	identityID string,
	identifier auth.Identifier,
	verifiedAt time.Time,
) error {
	identityID = strings.TrimSpace(identityID)
	if identityID == "" {
		return errors.New(
			"identity ID cannot be blank",
		)
	}

	normalized, err := auth.NewIdentifier(
		identifier.Type,
		identifier.Value,
	)
	if err != nil {
		return err
	}

	if verifiedAt.IsZero() {
		return errors.New(
			"identifier verification time cannot be zero",
		)
	}

	verifiedAt = verifiedAt.UTC()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf(
			"begin identity identifier linking transaction: %w",
			err,
		)
	}
	defer tx.Rollback(ctx)

	if err := lockIdentityIdentifier(
		ctx,
		tx,
		normalized,
	); err != nil {
		return err
	}

	const identityExistsQuery = `
		SELECT EXISTS (
			SELECT 1
			FROM identities
			WHERE id = $1
		)
	`

	var identityExists bool

	if err := tx.QueryRow(
		ctx,
		identityExistsQuery,
		identityID,
	).Scan(
		&identityExists,
	); err != nil {
		return fmt.Errorf(
			"check identity existence: %w",
			err,
		)
	}

	if !identityExists {
		return errors.New(
			"identity not found while linking identifier",
		)
	}

	const existingLinkQuery = `
		SELECT identity_id::text
		FROM identity_identifiers
		WHERE identifier_type = $1
		  AND normalized_value = $2
	`

	var existingIdentityID string

	err = tx.QueryRow(
		ctx,
		existingLinkQuery,
		string(normalized.Type),
		normalized.Value,
	).Scan(
		&existingIdentityID,
	)

	if err == nil {
		if existingIdentityID != identityID {
			return auth.ErrIdentifierAlreadyLinked
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf(
				"commit existing identifier link: %w",
				err,
			)
		}

		return nil
	}

	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf(
			"query existing identifier link: %w",
			err,
		)
	}

	const insertIdentifierQuery = `
		INSERT INTO identity_identifiers (
			identity_id,
			identifier_type,
			normalized_value,
			verified_at
		)
		VALUES ($1, $2, $3, $4)
	`

	if _, err := tx.Exec(
		ctx,
		insertIdentifierQuery,
		identityID,
		string(normalized.Type),
		normalized.Value,
		verifiedAt,
	); err != nil {
		if isIdentifierOwnershipConflict(err) {
			return auth.ErrIdentifierAlreadyLinked
		}

		return fmt.Errorf(
			"insert identity identifier link: %w",
			err,
		)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf(
			"commit identity identifier linking transaction: %w",
			err,
		)
	}

	return nil
}

func lockIdentityIdentifier(
	ctx context.Context,
	tx pgx.Tx,
	identifier auth.Identifier,
) error {
	lockKey := string(identifier.Type) +
		":" +
		identifier.Value

	const query = `
		SELECT pg_advisory_xact_lock(
			hashtextextended($1, 0)
		)
	`

	if _, err := tx.Exec(
		ctx,
		query,
		lockKey,
	); err != nil {
		return fmt.Errorf(
			"lock identity identifier: %w",
			err,
		)
	}

	return nil
}

func findIdentityByIdentifierTx(
	ctx context.Context,
	tx pgx.Tx,
	identifier auth.Identifier,
) (auth.Identity, bool, error) {
	const query = `
		SELECT
			i.id::text,
			i.status
		FROM identity_identifiers ii
		INNER JOIN identities i
			ON i.id = ii.identity_id
		WHERE ii.identifier_type = $1
		  AND ii.normalized_value = $2
	`

	var identity auth.Identity
	var status string

	err := tx.QueryRow(
		ctx,
		query,
		string(identifier.Type),
		identifier.Value,
	).Scan(
		&identity.ID,
		&status,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return auth.Identity{}, false, nil
	}

	if err != nil {
		return auth.Identity{}, false, fmt.Errorf(
			"query existing identity by identifier: %w",
			err,
		)
	}

	identity.IsActive = status == "active"

	return identity, true, nil
}

func isIdentifierOwnershipConflict(
	err error,
) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}

	return pgErr.Code == "23505" &&
		pgErr.ConstraintName ==
			"identity_identifiers_type_value_unique"
}

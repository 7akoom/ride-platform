package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	"github.com/jackc/pgx/v5/pgxpool"
)

type IdentityReader struct {
	pool *pgxpool.Pool
}

var _ auth.IdentityReader = (*IdentityReader)(nil)

func NewIdentityReader(
	pool *pgxpool.Pool,
) *IdentityReader {
	if pool == nil {
		panic("PostgreSQL pool is required")
	}

	return &IdentityReader{
		pool: pool,
	}
}

func (r *IdentityReader) FindByID(
	ctx context.Context,
	identityID string,
) (auth.IdentityDetails, bool, error) {
	identityID = strings.TrimSpace(identityID)
	if identityID == "" {
		return auth.IdentityDetails{}, false, errors.New(
			"identity ID is required",
		)
	}

	const query = `
		SELECT
			i.id::text,
			i.status,
			ii.identifier_type,
			ii.normalized_value,
			ii.verified_at
		FROM identities i
		LEFT JOIN identity_identifiers ii
			ON ii.identity_id = i.id
		WHERE i.id = $1::uuid
		ORDER BY
			ii.created_at ASC,
			ii.id ASC
	`

	rows, err := r.pool.Query(
		ctx,
		query,
		identityID,
	)
	if err != nil {
		return auth.IdentityDetails{}, false, fmt.Errorf(
			"query identity details: %w",
			err,
		)
	}
	defer rows.Close()

	var details auth.IdentityDetails
	found := false

	for rows.Next() {
		var (
			id              string
			statusValue     string
			identifierType  sql.NullString
			normalizedValue sql.NullString
			verifiedAt      sql.NullTime
		)

		if err := rows.Scan(
			&id,
			&statusValue,
			&identifierType,
			&normalizedValue,
			&verifiedAt,
		); err != nil {
			return auth.IdentityDetails{}, false, fmt.Errorf(
				"scan identity details: %w",
				err,
			)
		}

		if !found {
			status, err := auth.ParseIdentityStatus(statusValue)
			if err != nil {
				return auth.IdentityDetails{}, false, fmt.Errorf(
					"parse identity status: %w",
					err,
				)
			}

			details = auth.IdentityDetails{
				ID:          id,
				Status:      status,
				Identifiers: make([]auth.IdentityDetailsIdentifier, 0),
			}

			found = true
		}

		if !identifierType.Valid &&
			!normalizedValue.Valid &&
			!verifiedAt.Valid {
			continue
		}

		if !identifierType.Valid ||
			!normalizedValue.Valid ||
			!verifiedAt.Valid {
			return auth.IdentityDetails{}, false, errors.New(
				"identity identifier contains incomplete data",
			)
		}

		identifier, err := auth.NewIdentifier(
			auth.IdentifierType(identifierType.String),
			normalizedValue.String,
		)
		if err != nil {
			return auth.IdentityDetails{}, false, fmt.Errorf(
				"parse identity identifier: %w",
				err,
			)
		}

		details.Identifiers = append(
			details.Identifiers,
			auth.IdentityDetailsIdentifier{
				Identifier: identifier,
				VerifiedAt: verifiedAt.Time.UTC(),
			},
		)
	}

	if err := rows.Err(); err != nil {
		return auth.IdentityDetails{}, false, fmt.Errorf(
			"iterate identity details: %w",
			err,
		)
	}

	if !found {
		return auth.IdentityDetails{}, false, nil
	}

	return details, true, nil
}

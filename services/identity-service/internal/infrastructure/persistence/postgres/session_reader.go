package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SessionReader struct {
	pool *pgxpool.Pool
}

var _ auth.SessionReader = (*SessionReader)(nil)

func NewSessionReader(
	pool *pgxpool.Pool,
) *SessionReader {
	if pool == nil {
		panic("PostgreSQL pool is required")
	}

	return &SessionReader{
		pool: pool,
	}
}

func (r *SessionReader) ListActiveByIdentity(
	ctx context.Context,
	identityID string,
	now time.Time,
) ([]auth.SessionDetails, error) {
	if strings.TrimSpace(identityID) == "" {
		return nil, errors.New(
			"identity ID cannot be blank",
		)
	}

	if now.IsZero() {
		return nil, errors.New(
			"session lookup time cannot be zero",
		)
	}

	now = now.UTC()

	const query = `
		SELECT
			id::text,
			client_id,
			device_id,
			device_name,
			platform,
			app_version,
			host(ip_address),
			user_agent,
			expires_at,
			last_seen_at,
			created_at
		FROM auth_sessions
		WHERE identity_id = $1::uuid
		  AND revoked_at IS NULL
		  AND expires_at > $2
		ORDER BY created_at DESC, id DESC
	`

	rows, err := r.pool.Query(
		ctx,
		query,
		identityID,
		now,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"list active authentication sessions: %w",
			err,
		)
	}
	defer rows.Close()

	sessions := make([]auth.SessionDetails, 0)

	for rows.Next() {
		var session auth.SessionDetails

		if err := rows.Scan(
			&session.ID,
			&session.ClientID,
			&session.DeviceID,
			&session.DeviceName,
			&session.Platform,
			&session.AppVersion,
			&session.IPAddress,
			&session.UserAgent,
			&session.ExpiresAt,
			&session.LastSeenAt,
			&session.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf(
				"scan active authentication session: %w",
				err,
			)
		}

		session.ExpiresAt = session.ExpiresAt.UTC()
		session.CreatedAt = session.CreatedAt.UTC()

		if session.LastSeenAt != nil {
			lastSeenAt := session.LastSeenAt.UTC()
			session.LastSeenAt = &lastSeenAt
		}

		sessions = append(
			sessions,
			session,
		)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf(
			"iterate active authentication sessions: %w",
			err,
		)
	}

	return sessions, nil
}

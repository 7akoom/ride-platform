package postgres

import (
	"context"
	"fmt"
)

func (s *OutboxStore) PendingStats(ctx context.Context) (int64, float64, error) {
	const query = `
		SELECT count(*),
			COALESCE(GREATEST(EXTRACT(EPOCH FROM
				statement_timestamp() - min(occurred_at)), 0), 0)::double precision
		FROM outbox_events
		WHERE published_at IS NULL
	`
	var pending int64
	var oldestAgeSeconds float64
	if err := s.pool.QueryRow(ctx, query).Scan(&pending, &oldestAgeSeconds); err != nil {
		return 0, 0, fmt.Errorf("read outbox pending statistics: %w", err)
	}
	return pending, oldestAgeSeconds, nil
}

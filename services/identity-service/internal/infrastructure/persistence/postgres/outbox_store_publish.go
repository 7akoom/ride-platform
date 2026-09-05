package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	outboxapp "github.com/7akoom/ride-platform/services/identity-service/internal/application/outbox"
)

func (s *OutboxStore) MarkPublished(
	ctx context.Context,
	input outboxapp.MarkPublishedInput,
) (bool, error) {
	eventID := strings.TrimSpace(input.EventID)
	if eventID == "" {
		return false, errors.New(
			"outbox event ID cannot be blank",
		)
	}

	claimToken := strings.TrimSpace(input.ClaimToken)
	if claimToken == "" {
		return false, errors.New(
			"outbox claim token cannot be blank",
		)
	}

	if input.PublishedAt.IsZero() {
		return false, errors.New(
			"outbox publication time cannot be zero",
		)
	}

	publishedAt := input.PublishedAt.UTC()

	const markPublishedQuery = `
		UPDATE outbox_events
		SET
			published_at = $3,
			claim_token = NULL,
			claimed_at = NULL,
			last_error = NULL,
			updated_at = $3
		WHERE id = $1::uuid
		  AND claim_token = $2::uuid
		  AND published_at IS NULL
	`

	commandTag, err := s.pool.Exec(
		ctx,
		markPublishedQuery,
		eventID,
		claimToken,
		publishedAt,
	)
	if err != nil {
		return false, fmt.Errorf(
			"mark outbox event published: %w",
			err,
		)
	}

	switch commandTag.RowsAffected() {
	case 0:
		return false, nil

	case 1:
		return true, nil

	default:
		return false, errors.New(
			"mark outbox event published affected multiple rows",
		)
	}
}

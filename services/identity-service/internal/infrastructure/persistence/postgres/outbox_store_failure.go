package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	outboxapp "github.com/7akoom/ride-platform/services/identity-service/internal/application/outbox"
)

func (s *OutboxStore) MarkFailed(
	ctx context.Context,
	input outboxapp.MarkFailedInput,
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

	if input.FailedAt.IsZero() {
		return false, errors.New(
			"outbox failure time cannot be zero",
		)
	}

	if input.RetryAt.IsZero() {
		return false, errors.New(
			"outbox retry time cannot be zero",
		)
	}

	failedAt := input.FailedAt.UTC()
	retryAt := input.RetryAt.UTC()

	if retryAt.Before(failedAt) {
		return false, errors.New(
			"outbox retry time cannot be before failure time",
		)
	}

	errorMessage := strings.TrimSpace(
		input.ErrorMessage,
	)
	if errorMessage == "" {
		return false, errors.New(
			"outbox failure error message cannot be blank",
		)
	}

	const markFailedQuery = `
		UPDATE outbox_events
		SET
			claim_token = NULL,
			claimed_at = NULL,
			available_at = $3,
			last_error = $4,
			updated_at = $5
		WHERE id = $1::uuid
		  AND claim_token = $2::uuid
		  AND published_at IS NULL
	`

	commandTag, err := s.pool.Exec(
		ctx,
		markFailedQuery,
		eventID,
		claimToken,
		retryAt,
		errorMessage,
		failedAt,
	)
	if err != nil {
		return false, fmt.Errorf(
			"mark outbox event failed: %w",
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
			"mark outbox event failed affected multiple rows",
		)
	}
}

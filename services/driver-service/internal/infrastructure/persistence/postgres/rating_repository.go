package postgres

import (
	"context"
	"fmt"

	"github.com/7akoom/ride-platform/services/driver-service/internal/application/ratings"
)

// ApplyRating folds one rating into the profile's average and count, and records that this
// rating was counted, in ONE transaction. The processed_rating_events row goes in first: its
// primary key is what makes a redelivered event count once, however often it is delivered.
//
// The new average is (average x count + stars) / (count + 1), kept to two decimals. A profile
// starts at 5.00 with a count of 0, so the first real rating simply becomes the average: the
// starting 5.00 has no weight.
func (r *DriverRepository) ApplyRating(ctx context.Context, input ratings.ApplyInput) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	claimed, err := tx.Exec(
		ctx,
		`INSERT INTO processed_rating_events (rating_id)
         VALUES ($1)
         ON CONFLICT (rating_id) DO NOTHING`,
		input.RatingID,
	)
	if err != nil {
		return fmt.Errorf("record the rating as processed: %w", err)
	}

	if claimed.RowsAffected() == 0 {
		// Counted before: a repeat changes nothing.
		return nil
	}

	updated, err := tx.Exec(
		ctx,
		`UPDATE drivers
         SET rating_average = ROUND((rating_average * rating_count + $2::numeric) / (rating_count + 1), 2),
             rating_count = rating_count + 1
         WHERE id = $1`,
		input.RateeID,
		int64(input.Stars),
	)
	if err != nil {
		return fmt.Errorf("update the average: %w", err)
	}

	if updated.RowsAffected() == 0 {
		// The transaction is dropped, so the rating is not marked as processed either.
		return ratings.ErrProfileNotFound
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

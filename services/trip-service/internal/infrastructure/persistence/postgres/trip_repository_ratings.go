package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

const ratingUniqueViolationCode = "23505"

// CreateRating stores the rating and its trip.rated event in ONE transaction. The event
// carries the stars and who was rated, never the comment: the comment stays in this
// service. UNIQUE (trip_id, rated_by) is what turns a second rating by the same side
// into ErrAlreadyRated.
func (r *TripRepository) CreateRating(
	ctx context.Context,
	input trip.CreateRatingInput,
) (trip.Rating, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return trip.Rating{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var createdAt time.Time

	err = tx.QueryRow(
		ctx,
		`INSERT INTO trip_ratings
            (id, trip_id, rated_by, rater_id, ratee_id, stars, comment, tags)
         VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7::text, ''), $8)
         RETURNING created_at`,
		input.ID,
		input.TripID,
		string(input.RatedBy),
		input.RaterID,
		input.RateeID,
		input.Stars,
		input.Comment,
		nonNilTags(input.Tags),
	).Scan(&createdAt)
	if err != nil {
		var pgErr *pgconn.PgError

		if errors.As(err, &pgErr) && pgErr.Code == ratingUniqueViolationCode {
			return trip.Rating{}, trip.ErrAlreadyRated
		}

		return trip.Rating{}, fmt.Errorf("insert trip rating: %w", err)
	}

	if err := writeOutboxEvent(ctx, tx, "trip.rated", input.TripID, map[string]string{
		"rating_id": input.ID,
		"trip_id":   input.TripID,
		"rated_by":  string(input.RatedBy),
		"rater_id":  input.RaterID,
		"ratee_id":  input.RateeID,
		"stars":     strconv.Itoa(input.Stars),
		"tags":      strings.Join(input.Tags, ","),
	}); err != nil {
		return trip.Rating{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return trip.Rating{}, fmt.Errorf("commit transaction: %w", err)
	}

	return trip.Rating{
		ID:        input.ID,
		TripID:    input.TripID,
		RatedBy:   input.RatedBy,
		RaterID:   input.RaterID,
		RateeID:   input.RateeID,
		Stars:     input.Stars,
		Comment:   input.Comment,
		Tags:      input.Tags,
		CreatedAt: createdAt,
	}, nil
}

// FindRating is one side's rating of a trip. The comment is not read: it stays with
// the operating company.
func (r *TripRepository) FindRating(
	ctx context.Context,
	tripID string,
	ratedBy trip.RatedBy,
) (trip.Rating, bool, error) {
	rating := trip.Rating{TripID: tripID, RatedBy: ratedBy}

	err := r.pool.QueryRow(
		ctx,
		`SELECT id::text, rater_id::text, ratee_id::text, stars, created_at
           FROM trip_ratings
          WHERE trip_id = $1 AND rated_by = $2`,
		tripID,
		string(ratedBy),
	).Scan(&rating.ID, &rating.RaterID, &rating.RateeID, &rating.Stars, &rating.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return trip.Rating{}, false, nil
		}

		return trip.Rating{}, false, fmt.Errorf("select trip rating: %w", err)
	}

	return rating, true, nil
}

// nonNilTags: a nil slice would be NULL, and the column is NOT NULL.
func nonNilTags(tags []string) []string {
	if tags == nil {
		return []string{}
	}

	return tags
}

package trip

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// RatingWindow is how long after a trip completes either side can still rate it.
const RatingWindow = 24 * time.Hour

// MaxRatingCommentLength is the longest comment, in characters.
const MaxRatingCommentLength = 500

var (
	ErrInvalidRatedBy       = errors.New("rated_by must be rider or driver")
	ErrInvalidStars         = errors.New("stars must be a whole number from 1 to 5")
	ErrRatingCommentTooLong = errors.New("the comment is longer than 500 characters")
	ErrTripNotRatable       = errors.New("only a completed trip can be rated")
	ErrRatingWindowClosed   = errors.New("the time to rate this trip has passed")
	ErrAlreadyRated         = errors.New("this trip was already rated by this side")
)

// RatedBy is the side that gives the rating.
type RatedBy string

const (
	RatedByRider  RatedBy = "rider"
	RatedByDriver RatedBy = "driver"
)

func (r RatedBy) Valid() bool {
	return r == RatedByRider || r == RatedByDriver
}

// RateTripInput is what one side reports about the other.
type RateTripInput struct {
	TripID  string
	RatedBy RatedBy
	Stars   int
	Comment string
}

// CreateRatingInput is a validated rating, ready to be written.
type CreateRatingInput struct {
	ID      string
	TripID  string
	RatedBy RatedBy
	RaterID string
	RateeID string
	Stars   int
	Comment string
}

// Rating is a recorded rating.
type Rating struct {
	ID        string
	TripID    string
	RatedBy   RatedBy
	RaterID   string
	RateeID   string
	Stars     int
	Comment   string
	CreatedAt time.Time
}

// RateTrip records one side's rating of the other. The rider rates the driver and the
// driver rates the rider, after the trip is completed and within RatingWindow, once each.
// Who is being rated is taken from the trip, never from the request.
func (s *service) RateTrip(
	ctx context.Context,
	input RateTripInput,
) (Rating, error) {
	tripID := strings.TrimSpace(input.TripID)
	if tripID == "" {
		return Rating{}, ErrTripIDRequired
	}

	if !input.RatedBy.Valid() {
		return Rating{}, ErrInvalidRatedBy
	}

	if input.Stars < 1 || input.Stars > 5 {
		return Rating{}, ErrInvalidStars
	}

	comment := strings.TrimSpace(input.Comment)
	if utf8.RuneCountInString(comment) > MaxRatingCommentLength {
		return Rating{}, ErrRatingCommentTooLong
	}

	found, err := s.repository.FindByID(ctx, tripID)
	if err != nil {
		return Rating{}, fmt.Errorf("find trip: %w", err)
	}

	if found.Status != StatusCompleted {
		return Rating{}, ErrTripNotRatable
	}

	if found.CompletedAt == nil || time.Since(*found.CompletedAt) > RatingWindow {
		return Rating{}, ErrRatingWindowClosed
	}

	raterID, rateeID := found.RiderID, found.DriverID
	if input.RatedBy == RatedByDriver {
		raterID, rateeID = found.DriverID, found.RiderID
	}

	// A completed trip always has both; anything else is not something to rate.
	if raterID == "" || rateeID == "" {
		return Rating{}, ErrTripNotRatable
	}

	created, err := s.repository.CreateRating(ctx, CreateRatingInput{
		ID:      s.idGenerator.NewID(),
		TripID:  found.ID,
		RatedBy: input.RatedBy,
		RaterID: raterID,
		RateeID: rateeID,
		Stars:   input.Stars,
		Comment: comment,
	})
	if err != nil {
		return Rating{}, fmt.Errorf("create rating: %w", err)
	}

	return created, nil
}

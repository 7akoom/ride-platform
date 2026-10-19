package trip

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// RatingStatus is one side's own rating of a trip, as the trip's details show it.
type RatingStatus struct {
	Rated bool
	// 1 to 5 when Rated.
	Stars int
	// CanRate: not rated, and RateTrip would take it now.
	CanRate bool
	// CanRateUntil is set when CanRate.
	CanRateUntil *time.Time
}

// TripRating reads one side's rating of a trip. The comment is left out: the person
// rated never sees it, and the app does not need it back.
func (s *service) TripRating(ctx context.Context, tripID string, ratedBy RatedBy) (RatingStatus, error) {
	tripID = strings.TrimSpace(tripID)
	if tripID == "" {
		return RatingStatus{}, ErrTripIDRequired
	}

	if !ratedBy.Valid() {
		return RatingStatus{}, ErrInvalidRatedBy
	}

	found, err := s.repository.FindByID(ctx, tripID)
	if err != nil {
		return RatingStatus{}, fmt.Errorf("find trip: %w", err)
	}

	rating, rated, err := s.repository.FindRating(ctx, found.ID, ratedBy)
	if err != nil {
		return RatingStatus{}, fmt.Errorf("find rating: %w", err)
	}

	if rated {
		return RatingStatus{Rated: true, Stars: rating.Stars}, nil
	}

	return ratingWindow(found, time.Now()), nil
}

// ratingWindow says whether a trip nobody on this side has rated can still be rated
// at now, with the same rules as RateTrip.
func ratingWindow(found Trip, now time.Time) RatingStatus {
	if found.Status != StatusCompleted || found.CompletedAt == nil || found.RiderID == "" || found.DriverID == "" {
		return RatingStatus{}
	}

	until := found.CompletedAt.Add(RatingWindow)
	if !now.Before(until) {
		return RatingStatus{}
	}

	return RatingStatus{CanRate: true, CanRateUntil: &until}
}

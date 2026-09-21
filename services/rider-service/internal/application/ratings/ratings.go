package ratings

import (
	"context"
	"errors"
)

// SubjectTripRated is the JetStream subject trip-service's outbox publishes when one side
// has rated the other.
const SubjectTripRated = "trip.rated"

// Which side gave the rating. The event says who was rated only through this: a rating
// given BY a rider is about a driver, and one given BY a driver is about a rider.
const (
	RatedByRider  = "rider"
	RatedByDriver = "driver"
)

// ErrProfileNotFound: the profile the rating is about does not exist here.
var ErrProfileNotFound = errors.New("the rated profile does not exist")

// ApplyInput is one rating to fold into a profile's running average.
type ApplyInput struct {
	RatingID string
	RateeID  string
	Stars    int
}

// RatingApplier folds a rating into a profile's average and count, and remembers that this
// rating was counted so that a redelivered event cannot count it twice. Both happen in one
// transaction. A rating that was already counted returns nil and changes nothing.
type RatingApplier interface {
	ApplyRating(ctx context.Context, input ApplyInput) error
}

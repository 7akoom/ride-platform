package ratings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"time"
)

// retryDelay is how long a failed rating waits before JetStream redelivers it, so a database
// that is down does not turn into a hot redelivery loop.
const retryDelay = 5 * time.Second

var uuidShape = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// retryLaterError asks the JetStream consumer to redeliver after retryDelay. The consumer
// finds RetryDelay through an interface check, so the infrastructure layer never imports
// this package.
type retryLaterError struct{ cause error }

func (e *retryLaterError) Error() string {
	return fmt.Sprintf("retry in %s: %v", retryDelay, e.cause)
}

func (e *retryLaterError) Unwrap() error { return e.cause }

func (e *retryLaterError) RetryDelay() time.Duration { return retryDelay }

// envelope is the JSON shape every outbox publisher wraps its message in.
type envelope struct {
	EventID string          `json:"event_id"`
	Payload json.RawMessage `json:"payload"`
}

// payload is the trip.rated payload. Every value is a string, stars included.
type payload struct {
	RatingID string `json:"rating_id"`
	RatedBy  string `json:"rated_by"`
	RateeID  string `json:"ratee_id"`
	Stars    string `json:"stars"`
}

// Handler turns trip.rated events into changes to this service's own profiles: it handles
// the ratings GIVEN BY ratedBy (the other side's), and ignores the rest.
type Handler struct {
	applier RatingApplier
	ratedBy string
	logger  *slog.Logger
}

func NewHandler(applier RatingApplier, ratedBy string, logger *slog.Logger) *Handler {
	if applier == nil {
		panic("rating applier is required")
	}

	if ratedBy != RatedByRider && ratedBy != RatedByDriver {
		panic("rated by must be rider or driver")
	}

	if logger == nil {
		panic("logger is required")
	}

	return &Handler{applier: applier, ratedBy: ratedBy, logger: logger}
}

// Handle processes one JetStream message. nil acks it (done, or nothing worth doing here);
// a retryLaterError redelivers it later. Anything that can never succeed (a message that
// cannot be read, a rating out of range) is logged and acked, so it does not come back
// forever.
func (h *Handler) Handle(ctx context.Context, subject string, data []byte) error {
	if subject != SubjectTripRated {
		return nil
	}

	var env envelope

	if err := json.Unmarshal(data, &env); err != nil {
		h.logger.ErrorContext(ctx, "dropping an unreadable trip.rated event", "error", err)

		return nil
	}

	var p payload

	if err := json.Unmarshal(env.Payload, &p); err != nil {
		h.logger.ErrorContext(ctx, "dropping a trip.rated event with an unreadable payload",
			"event_id", env.EventID,
			"error", err,
		)

		return nil
	}

	// The other side's rating: not for this service.
	if p.RatedBy != h.ratedBy {
		return nil
	}

	stars, err := strconv.Atoi(p.Stars)
	if err != nil || stars < 1 || stars > 5 || !uuidShape.MatchString(p.RatingID) || !uuidShape.MatchString(p.RateeID) {
		h.logger.ErrorContext(ctx, "dropping a trip.rated event that cannot be applied",
			"event_id", env.EventID,
			"rating_id", p.RatingID,
			"ratee_id", p.RateeID,
			"stars", p.Stars,
		)

		return nil
	}

	err = h.applier.ApplyRating(ctx, ApplyInput{RatingID: p.RatingID, RateeID: p.RateeID, Stars: stars})

	switch {
	case err == nil:
		h.logger.InfoContext(ctx, "rating applied",
			"rating_id", p.RatingID,
			"ratee_id", p.RateeID,
			"stars", stars,
		)

		return nil

	case errors.Is(err, ErrProfileNotFound):
		// Retrying cannot create the profile.
		h.logger.WarnContext(ctx, "rating is about a profile that does not exist",
			"rating_id", p.RatingID,
			"ratee_id", p.RateeID,
		)

		return nil

	default:
		h.logger.WarnContext(ctx, "applying a rating failed; will retry",
			"rating_id", p.RatingID,
			"error", err,
		)

		return &retryLaterError{cause: err}
	}
}

package events

import (
	"context"
	"errors"
	"time"

	"github.com/7akoom/ride-platform/services/dispatch-service/internal/application/dispatch"
)

// maxOfferPollInterval is how often a trip whose offer is being answered is looked
// at again. It is short so that a rejection or an expiry is noticed within seconds
// and the next driver is offered without a long silence for the rider, while
// "no driver available yet" keeps the slower retry interval.
const maxOfferPollInterval = 2 * time.Second

var errOfferAwaitingAnswer = errors.New("the offered driver has not answered yet")

// awaitOffer is what a successful DispatchTrip means when dispatch works by
// offers: the trip was put to a driver, not assigned. Nothing is done with the
// trip until the offer is answered, so the event is looked at again soon; the
// next attempt sees the trip accepted (and stops), or rejected or expired (and
// offers it to the next driver).
func (h *Handler) awaitOffer(ctx context.Context, result dispatch.Result) error {
	delay := min(h.retryInterval, maxOfferPollInterval)

	h.logger.InfoContext(ctx, "trip offered to a driver; waiting for the answer",
		"trip_id", result.TripID,
		"driver_id", result.DriverID,
		"distance_meters", result.DistanceMeters,
		"look_again_in", delay,
	)

	return &retryLaterError{delay: delay, cause: errOfferAwaitingAnswer}
}

// awaitPendingOffer is the same wait when this attempt found that another driver's
// offer of the trip is still live.
func (h *Handler) awaitPendingOffer(ctx context.Context, tripID string) error {
	delay := min(h.retryInterval, maxOfferPollInterval)

	h.logger.DebugContext(ctx, "the trip has a live offer; will look again",
		"trip_id", tripID,
		"look_again_in", delay,
	)

	return &retryLaterError{delay: delay, cause: dispatch.ErrOfferPending}
}

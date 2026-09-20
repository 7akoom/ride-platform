package clients

import (
	"context"
	"fmt"
	"time"

	tripv1 "github.com/7akoom/ride-platform/gen/go/ride/trip/v1"
	"github.com/7akoom/ride-platform/services/dispatch-service/internal/application/dispatch"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// OfferTrip puts the trip to one driver for ttl. trip-service answers ABORTED when
// another driver has a live offer (wait: dispatch.ErrOfferPending), and
// ALREADY_EXISTS or FAILED_PRECONDITION when this driver cannot be offered the trip
// (try the next one: dispatch.ErrDriverNotOfferable).
func (c *TripClient) OfferTrip(
	ctx context.Context,
	tripID string,
	driverID string,
	ttl time.Duration,
) error {
	_, err := c.client.OfferTrip(ctx, &tripv1.OfferTripRequest{
		TripId:     tripID,
		DriverId:   driverID,
		TtlSeconds: int32(ttl / time.Second),
	})
	if err == nil {
		return nil
	}

	switch status.Code(err) {
	case codes.Aborted:
		return fmt.Errorf("%w: %v", dispatch.ErrOfferPending, err)

	case codes.AlreadyExists, codes.FailedPrecondition:
		return fmt.Errorf("%w: %v", dispatch.ErrDriverNotOfferable, err)

	default:
		return fmt.Errorf("call trip-service OfferTrip: %w", err)
	}
}

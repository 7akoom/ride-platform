package clients

import (
	"context"
	"fmt"

	riderv1 "github.com/7akoom/ride-platform/gen/go/ride/rider/v1"
	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// RiderDirectory reads a rider's profile from rider-service with the internal service
// token: a driver may not read it there, and trip-service has already checked that this
// driver is on this trip. Only the fields a driver may see are copied out.
type RiderDirectory struct {
	client riderv1.RiderServiceClient
}

func NewRiderDirectory(conn *grpc.ClientConn) *RiderDirectory {
	if conn == nil {
		panic("rider-service connection is required")
	}

	return &RiderDirectory{client: riderv1.NewRiderServiceClient(conn)}
}

func (d *RiderDirectory) RiderProfile(ctx context.Context, riderID string) (trip.RiderProfile, error) {
	response, err := d.client.GetRider(ctx, &riderv1.GetRiderRequest{RiderId: riderID})
	if status.Code(err) == codes.NotFound {
		return trip.RiderProfile{}, trip.ErrRiderProfileUnavailable
	}

	if err != nil {
		return trip.RiderProfile{}, fmt.Errorf("call rider-service GetRider: %w", err)
	}

	rider := response.GetRider()

	return trip.RiderProfile{
		DisplayName:   rider.GetDisplayName(),
		RatingAverage: rider.GetRatingAverage(),
		RatingCount:   rider.GetRatingCount(),
	}, nil
}

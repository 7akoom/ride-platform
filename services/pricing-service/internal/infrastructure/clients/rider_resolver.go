package clients

import (
	"context"
	"fmt"

	riderv1 "github.com/7akoom/ride-platform/gen/go/ride/rider/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// RiderResolver asks rider-service which rider profile an identity owns.
type RiderResolver struct {
	riders riderv1.RiderServiceClient
}

func NewRiderResolver(riderConn *grpc.ClientConn) *RiderResolver {
	if riderConn == nil {
		panic("rider-service connection is required")
	}

	return &RiderResolver{riders: riderv1.NewRiderServiceClient(riderConn)}
}

func (r *RiderResolver) RiderID(ctx context.Context, identityID string) (string, error) {
	response, err := r.riders.GetRiderByIdentity(ctx, &riderv1.GetRiderByIdentityRequest{IdentityId: identityID})
	if status.Code(err) == codes.NotFound {
		return "", nil
	}

	if err != nil {
		return "", fmt.Errorf("call rider-service GetRiderByIdentity: %w", err)
	}

	return response.GetRider().GetId(), nil
}

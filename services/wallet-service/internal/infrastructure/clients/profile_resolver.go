package clients

import (
	"context"
	"fmt"

	driverv1 "github.com/7akoom/ride-platform/gen/go/ride/driver/v1"
	riderv1 "github.com/7akoom/ride-platform/gen/go/ride/rider/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ProfileResolver struct {
	riders  riderv1.RiderServiceClient
	drivers driverv1.DriverServiceClient
}

func NewProfileResolver(riderConn, driverConn *grpc.ClientConn) *ProfileResolver {
	if riderConn == nil || driverConn == nil {
		panic("rider-service and driver-service connections are required")
	}

	return &ProfileResolver{
		riders:  riderv1.NewRiderServiceClient(riderConn),
		drivers: driverv1.NewDriverServiceClient(driverConn),
	}
}

func (r *ProfileResolver) RiderID(ctx context.Context, identityID string) (string, error) {
	response, err := r.riders.GetRiderByIdentity(ctx, &riderv1.GetRiderByIdentityRequest{IdentityId: identityID})
	if status.Code(err) == codes.NotFound {
		return "", nil
	}

	if err != nil {
		return "", fmt.Errorf("call rider-service GetRiderByIdentity: %w", err)
	}

	return response.GetRider().GetId(), nil
}

func (r *ProfileResolver) DriverID(ctx context.Context, identityID string) (string, error) {
	response, err := r.drivers.GetDriverByIdentity(ctx, &driverv1.GetDriverByIdentityRequest{IdentityId: identityID})
	if status.Code(err) == codes.NotFound {
		return "", nil
	}

	if err != nil {
		return "", fmt.Errorf("call driver-service GetDriverByIdentity: %w", err)
	}

	return response.GetDriver().GetId(), nil
}

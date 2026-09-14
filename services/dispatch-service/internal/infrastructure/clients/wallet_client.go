package clients

import (
	"context"
	"fmt"

	walletv1 "github.com/7akoom/ride-platform/gen/go/ride/wallet/v1"
	"github.com/7akoom/ride-platform/services/dispatch-service/internal/application/dispatch"
	"google.golang.org/grpc"
)

type WalletClient struct {
	client walletv1.WalletServiceClient
}

func NewWalletClient(conn *grpc.ClientConn) *WalletClient {
	if conn == nil {
		panic("wallet-service connection is required")
	}

	return &WalletClient{client: walletv1.NewWalletServiceClient(conn)}
}

func (c *WalletClient) CheckDriverStanding(
	ctx context.Context,
	driverID string,
) (dispatch.DriverStanding, error) {
	response, err := c.client.CheckDriverStanding(ctx, &walletv1.CheckDriverStandingRequest{
		DriverId: driverID,
	})
	if err != nil {
		return dispatch.DriverStanding{}, fmt.Errorf("call wallet-service CheckDriverStanding: %w", err)
	}

	return dispatch.DriverStanding{
		CanTakeTrips: response.GetCanTakeTrips(),
		Suspended:    response.GetSuspended(),
		Reason:       response.GetReason(),
	}, nil
}

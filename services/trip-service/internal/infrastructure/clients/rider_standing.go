package clients

import (
	"context"
	"fmt"
	"log/slog"

	walletv1 "github.com/7akoom/ride-platform/gen/go/ride/wallet/v1"
	"google.golang.org/grpc"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

// RiderStanding reads a rider's unpaid fees from wallet-service, as a service
// (the connection carries the internal token).
type RiderStanding struct {
	wallet walletv1.WalletServiceClient
	logger *slog.Logger
}

var _ trip.RiderStanding = (*RiderStanding)(nil)

func NewRiderStanding(walletConn grpc.ClientConnInterface, logger *slog.Logger) *RiderStanding {
	if walletConn == nil {
		panic("wallet-service connection is required")
	}

	if logger == nil {
		panic("logger is required")
	}

	return &RiderStanding{wallet: walletv1.NewWalletServiceClient(walletConn), logger: logger}
}

// RiderStanding: any failure to read it is ErrUpstreamUnavailable, logged,
// and the trip service lets the rider through.
func (r *RiderStanding) RiderStanding(ctx context.Context, riderID string) (trip.Standing, error) {
	ctx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()

	response, err := r.wallet.GetRiderDues(ctx, &walletv1.GetRiderDuesRequest{RiderId: riderID})
	if err != nil {
		r.logger.WarnContext(ctx, "could not read the rider's unpaid fees; letting the trip request through", "error", err)

		return trip.Standing{}, fmt.Errorf("%w: wallet-service GetRiderDues: %v", trip.ErrUpstreamUnavailable, err)
	}

	return trip.Standing{
		CanRequestTrips: response.GetCanRequestTrips(),
		Outstanding:     response.GetOutstanding(),
		CurrencyCode:    response.GetCurrencyCode(),
	}, nil
}

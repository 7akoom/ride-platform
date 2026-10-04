package clients

import (
	"context"
	"fmt"
	"strings"
	"time"

	walletv1 "github.com/7akoom/ride-platform/gen/go/ride/wallet/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/feed"
)

// WalletFeed reads wallet movements for the activity page (feed.Wallet), with
// the internal service token on the connection.
type WalletFeed struct {
	client walletv1.WalletServiceClient
}

func NewWalletFeed(conn *grpc.ClientConn) *WalletFeed {
	if conn == nil {
		panic("wallet-service connection is required")
	}

	return &WalletFeed{client: walletv1.NewWalletServiceClient(conn)}
}

func (w *WalletFeed) MovementsBefore(
	ctx context.Context,
	owner feed.Owner,
	ownerID string,
	before *feed.Cursor,
	limit int,
) ([]feed.Movement, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	request := &walletv1.ListFeedTransactionsRequest{
		OwnerType: walletv1.OwnerType_OWNER_TYPE_RIDER, OwnerId: ownerID, Limit: int32(limit), ExcludeTrips: true,
	}

	if owner == feed.OwnerDriver {
		request.OwnerType = walletv1.OwnerType_OWNER_TYPE_DRIVER
	}

	if before != nil {
		request.Before = timestamppb.New(before.At)
		request.BeforeId = before.ID
	}

	response, err := w.client.ListFeedTransactions(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", feed.ErrWalletUnavailable, err)
	}

	out := make([]feed.Movement, 0, len(response.GetTransactions()))

	for _, t := range response.GetTransactions() {
		out = append(out, feed.Movement{
			ID:           t.GetId(),
			Type:         strings.ToLower(strings.TrimPrefix(t.GetType().String(), "TRANSACTION_TYPE_")),
			Amount:       t.GetAmount(),
			BalanceAfter: t.GetBalanceAfter(),
			Currency:     response.GetCurrencyCode(),
			Description:  t.GetDescription(),
			TransferID:   t.GetTransferId(),
			At:           t.GetCreatedAt().AsTime(),
		})
	}

	return out, nil
}

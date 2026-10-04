package grpc

import (
	"context"
	"errors"

	tripv1 "github.com/7akoom/ride-platform/gen/go/ride/trip/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/feed"
)

// WithFeed serves the activity page.
func WithFeed(service *feed.Service) HandlerOption {
	return func(h *TripHandler) { h.feed = service }
}

// ListActivityFeed: the interceptor has checked the profile is the caller's.
func (h *TripHandler) ListActivityFeed(
	ctx context.Context,
	request *tripv1.ListActivityFeedRequest,
) (*tripv1.ListActivityFeedResponse, error) {
	if h.feed == nil {
		return nil, status.Error(codes.Unimplemented, "the activity page is not available")
	}

	page, err := h.feed.List(ctx, request.GetRiderId(), request.GetDriverId(), int(request.GetPageSize()), request.GetPageToken())

	switch {
	case errors.Is(err, feed.ErrInvalidQuery), errors.Is(err, feed.ErrInvalidPageToken):
		return nil, status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, feed.ErrWalletUnavailable):
		h.logger.Warn("activity page without the wallet", "error", err)

		return nil, status.Error(codes.Unavailable, feed.ErrWalletUnavailable.Error())
	case err != nil:
		h.logger.Error("activity page failed", "error", err)

		return nil, status.Error(codes.Internal, "failed to list activity")
	}

	response := &tripv1.ListActivityFeedResponse{NextPageToken: page.NextPageToken}

	for _, it := range page.Items {
		item := &tripv1.ActivityItem{Id: it.ID, OccurredAt: timestamppb.New(it.At)}

		switch it.Kind {
		case feed.KindTrip:
			item.Kind = tripv1.ActivityItemKind_ACTIVITY_ITEM_KIND_TRIP
			item.Trip = toProtoTrip(*it.Trip)
		case feed.KindWallet:
			m := it.Movement
			item.Kind = tripv1.ActivityItemKind_ACTIVITY_ITEM_KIND_WALLET
			item.Wallet = &tripv1.WalletActivity{
				TransactionId: m.ID, Type: m.Type, Amount: m.Amount, BalanceAfter: m.BalanceAfter,
				CurrencyCode: m.Currency, Description: m.Description, TransferId: m.TransferID,
			}
		}

		response.Items = append(response.Items, item)
	}

	return response, nil
}

package grpc

import (
	"context"

	walletv1 "github.com/7akoom/ride-platform/gen/go/ride/wallet/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

// FeedReader lists a wallet's movements for the activity page.
type FeedReader interface {
	ListFeedTransactions(ctx context.Context, query wallet.FeedQuery) ([]wallet.Transaction, string, error)
}

// WithFeed answers ListFeedTransactions (trip-service's activity page).
func (h *WalletHandler) WithFeed(reader FeedReader) *WalletHandler {
	h.feed = reader

	return h
}

func (h *WalletHandler) ListFeedTransactions(
	ctx context.Context,
	request *walletv1.ListFeedTransactionsRequest,
) (*walletv1.ListFeedTransactionsResponse, error) {
	if h.feed == nil {
		return nil, status.Error(codes.Unimplemented, "the wallet feed is not available")
	}

	owner := toDomainOwnerType(request.GetOwnerType())
	limit := int(request.GetLimit())

	switch {
	case !owner.Valid() || !isUUIDString(request.GetOwnerId()):
		return nil, status.Error(codes.InvalidArgument, "owner_type and owner_id are required")
	case limit < 1 || limit > 100:
		return nil, status.Error(codes.InvalidArgument, "limit is 1 to 100")
	case request.GetBeforeId() != "" && !isUUIDString(request.GetBeforeId()):
		return nil, status.Error(codes.InvalidArgument, "before_id is an id")
	}

	query := wallet.FeedQuery{
		OwnerType: owner, OwnerID: request.GetOwnerId(), Limit: limit, ExcludeTrips: request.GetExcludeTrips(),
	}

	if request.GetBefore() != nil {
		before := request.GetBefore().AsTime()
		query.Before = &before
		query.BeforeID = request.GetBeforeId()
	}

	found, currency, err := h.feed.ListFeedTransactions(ctx, query)
	if err != nil {
		h.logger.Error("list feed transactions failed", "error", err)

		return nil, status.Error(codes.Internal, "failed to list transactions")
	}

	response := &walletv1.ListFeedTransactionsResponse{CurrencyCode: currency}
	for _, t := range found {
		response.Transactions = append(response.Transactions, toProtoTransaction(t))
	}

	return response, nil
}

func isUUIDString(value string) bool {
	if len(value) != 36 {
		return false
	}

	for i, r := range value {
		switch {
		case i == 8 || i == 13 || i == 18 || i == 23:
			if r != '-' {
				return false
			}
		case (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F'):
			return false
		}
	}

	return true
}

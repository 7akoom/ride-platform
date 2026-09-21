package grpc

import (
	"context"

	walletv1 "github.com/7akoom/ride-platform/gen/go/ride/wallet/v1"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// RecordTripChange is how the driver reports the cash the rider actually handed over. The
// driver sends one number; the change is worked out on the server.
func (h *WalletHandler) RecordTripChange(
	ctx context.Context,
	request *walletv1.RecordTripChangeRequest,
) (*walletv1.RecordTripChangeResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	received, err := parseMoney(request.GetCashReceived())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "cash_received must be a valid decimal value")
	}

	credit, err := h.walletService.RecordTripChange(ctx, wallet.RecordTripChangeInput{
		DriverID:     request.GetDriverId(),
		TripID:       request.GetTripId(),
		CashReceived: received,
	})
	if err != nil {
		return nil, h.mapWalletError(err)
	}

	return &walletv1.RecordTripChangeResponse{
		TripId:       credit.TripID,
		CurrencyCode: credit.CurrencyCode,
		CashDue:      credit.CashDue.String(),
		CashReceived: credit.CashReceived.String(),
		ChangeAmount: credit.ChangeAmount.String(),
	}, nil
}

// transactionTypeForProto adds the change credit to the ledger types the handler knows;
// every other type keeps its existing mapping.
func transactionTypeForProto(t wallet.TransactionType) walletv1.TransactionType {
	if t == wallet.TxChangeCredit {
		return walletv1.TransactionType_TRANSACTION_TYPE_CHANGE_CREDIT
	}

	return toProtoTransactionType(t)
}

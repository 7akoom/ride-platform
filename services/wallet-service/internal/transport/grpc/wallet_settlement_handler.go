package grpc

import (
	"context"

	walletv1 "github.com/7akoom/ride-platform/gen/go/ride/wallet/v1"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GetTripSettlement tells the rider what was taken from their wallet and the
// driver how much cash to collect. The commission and the driver's earning are
// the driver's business: a rider does not get them.
func (h *WalletHandler) GetTripSettlement(
	ctx context.Context,
	request *walletv1.GetTripSettlementRequest,
) (*walletv1.GetTripSettlementResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	ownerType := toDomainOwnerType(request.GetOwnerType())

	found, err := h.walletService.GetTripSettlement(ctx, ownerType, request.GetOwnerId(), request.GetTripId())
	if err != nil {
		return nil, h.mapWalletError(err)
	}

	response := &walletv1.GetTripSettlementResponse{
		TripId:        found.TripID,
		PaymentMethod: settlementPaymentMethod(found.PaymentMethod),
		CurrencyCode:  found.CurrencyCode,
		FareAmount:    found.FareAmount.String(),
		WalletAmount:  found.WalletAmount.String(),
		CashAmount:    found.CashAmount.String(),
		ChangeAmount:  found.ChangeAmount.String(),
	}

	if ownerType == wallet.OwnerDriver {
		response.CommissionAmount = found.CommissionAmount.String()
		response.DriverEarning = found.DriverEarning.String()
	}

	return response, nil
}

func settlementPaymentMethod(p wallet.PaymentMethod) walletv1.PaymentMethod {
	switch p {
	case wallet.PaymentCash:
		return walletv1.PaymentMethod_PAYMENT_METHOD_CASH
	case wallet.PaymentWallet:
		return walletv1.PaymentMethod_PAYMENT_METHOD_WALLET
	case wallet.PaymentCard:
		return walletv1.PaymentMethod_PAYMENT_METHOD_CARD
	default:
		return walletv1.PaymentMethod_PAYMENT_METHOD_UNSPECIFIED
	}
}

package grpc

import (
	"context"
	"time"

	walletv1 "github.com/7akoom/ride-platform/gen/go/ride/wallet/v1"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/operations"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// CashTopUp credits a wallet with cash paid at the office. The interceptor has
// checked the caller holds wallets.cash_topup.
func (h *WalletHandler) CashTopUp(
	ctx context.Context,
	request *walletv1.CashTopUpRequest,
) (*walletv1.AdjustmentResponse, error) {
	if h.operations == nil {
		return nil, errOperationsUnavailable
	}

	staff, err := staffActor(ctx, "", "cash is credited by a staff member")
	if err != nil {
		return nil, err
	}

	amount, err := parseMoney(request.GetAmount())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, operations.ErrInvalidCashAmount.Error())
	}

	made, balance, err := h.operations.CashTopUp(ctx, operations.CashTopUpInput{
		StaffIdentityID: staff,
		OwnerType:       toDomainOwnerType(request.GetOwnerType()),
		OwnerID:         request.GetOwnerId(),
		Amount:          amount,
		ReceiptNumber:   request.GetReceiptNumber(),
		Note:            request.GetNote(),
		IdempotencyKey:  request.GetIdempotencyKey(),
	})
	if err != nil {
		return nil, h.mapOperationsError(err)
	}

	return h.adjustmentResponse(ctx, made, balance)
}

// ListCashTopUps is the office's cash in a range, with its total.
func (h *WalletHandler) ListCashTopUps(
	ctx context.Context,
	request *walletv1.ListCashTopUpsRequest,
) (*walletv1.ListCashTopUpsResponse, error) {
	if h.operations == nil {
		return nil, errOperationsUnavailable
	}

	var from, to time.Time

	if request.GetFrom() != nil {
		from = request.GetFrom().AsTime()
	}

	if request.GetTo() != nil {
		to = request.GetTo().AsTime()
	}

	found, err := h.operations.ListCashTopUps(ctx, from, to, request.GetCreatedBy())
	if err != nil {
		return nil, h.mapOperationsError(err)
	}

	response := &walletv1.ListCashTopUpsResponse{
		TotalAmount:  found.Total.String(),
		Count:        int32(found.Count),
		CurrencyCode: found.CurrencyCode,
		Truncated:    found.Count > len(found.TopUps),
	}

	for _, topUp := range found.TopUps {
		response.CashTopUps = append(response.CashTopUps, toProtoAdjustment(topUp))
	}

	return response, nil
}

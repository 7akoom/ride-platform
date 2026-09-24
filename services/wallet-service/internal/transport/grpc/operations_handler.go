package grpc

import (
	"context"
	"errors"
	"strconv"

	walletv1 "github.com/7akoom/ride-platform/gen/go/ride/wallet/v1"
	"github.com/shopspring/decimal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/operations"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

// WithOperations gives the handler staff money operations and payouts.
func (h *WalletHandler) WithOperations(ops *operations.Service) *WalletHandler {
	h.operations = ops

	return h
}

var errOperationsUnavailable = status.Error(codes.Unimplemented, "wallet operations are not available")

// RequestPayout holds the amount and opens a payout request for staff.
func (h *WalletHandler) RequestPayout(
	ctx context.Context,
	request *walletv1.RequestPayoutRequest,
) (*walletv1.RequestPayoutResponse, error) {
	if h.operations == nil {
		return nil, errOperationsUnavailable
	}

	amount, err := parseMoney(request.GetAmount())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "amount must be a valid decimal value")
	}

	payout, balance, hold, err := h.operations.RequestPayout(ctx, operations.PayoutInput{
		DriverID:       request.GetDriverId(),
		Amount:         amount,
		Destination:    request.GetDestination(),
		IdempotencyKey: request.GetIdempotencyKey(),
	})
	if err != nil {
		return nil, h.mapOperationsError(err)
	}

	response := &walletv1.RequestPayoutResponse{Payout: toProtoPayout(payout)}

	// A retry returns the request made the first time: the wallet as it is
	// now, and no new ledger row.
	if balance.ID == "" {
		if balance, err = h.walletService.GetWallet(ctx, wallet.OwnerDriver, request.GetDriverId()); err != nil {
			return nil, h.mapWalletError(err)
		}
	} else {
		response.Transaction = toProtoTransaction(hold)
	}

	response.Wallet = toProtoWallet(balance)

	return response, nil
}

func (h *WalletHandler) ListPayouts(
	ctx context.Context,
	request *walletv1.ListPayoutsRequest,
) (*walletv1.ListPayoutsResponse, error) {
	if h.operations == nil {
		return nil, errOperationsUnavailable
	}

	if request.GetDriverId() == "" {
		return nil, status.Error(codes.InvalidArgument, operations.ErrDriverRequired.Error())
	}

	page, err := h.operations.ListPayouts(ctx, request.GetDriverId(), "", int(request.GetPageSize()), request.GetPageToken())
	if err != nil {
		return nil, h.mapOperationsError(err)
	}

	return toProtoPayoutPage(page), nil
}

func (h *WalletHandler) ListPayoutRequests(
	ctx context.Context,
	request *walletv1.ListPayoutRequestsRequest,
) (*walletv1.ListPayoutsResponse, error) {
	if h.operations == nil {
		return nil, errOperationsUnavailable
	}

	page, err := h.operations.ListPayouts(ctx, "", request.GetStatus(), int(request.GetPageSize()), request.GetPageToken())
	if err != nil {
		return nil, h.mapOperationsError(err)
	}

	return toProtoPayoutPage(page), nil
}

func (h *WalletHandler) ApprovePayout(ctx context.Context, request *walletv1.ApprovePayoutRequest) (*walletv1.PayoutResponse, error) {
	return h.payoutAction(ctx, func(staff string) (operations.Payout, error) {
		return h.operations.ApprovePayout(ctx, staff, request.GetPayoutId())
	})
}

func (h *WalletHandler) MarkPayoutPaid(ctx context.Context, request *walletv1.MarkPayoutPaidRequest) (*walletv1.PayoutResponse, error) {
	return h.payoutAction(ctx, func(staff string) (operations.Payout, error) {
		return h.operations.MarkPayoutPaid(ctx, staff, request.GetPayoutId(), request.GetReference())
	})
}

func (h *WalletHandler) RejectPayout(ctx context.Context, request *walletv1.RejectPayoutRequest) (*walletv1.PayoutResponse, error) {
	return h.payoutAction(ctx, func(staff string) (operations.Payout, error) {
		return h.operations.RejectPayout(ctx, staff, request.GetPayoutId(), request.GetReason())
	})
}

func (h *WalletHandler) payoutAction(ctx context.Context, act func(staff string) (operations.Payout, error)) (*walletv1.PayoutResponse, error) {
	if h.operations == nil {
		return nil, errOperationsUnavailable
	}

	staff, err := personFrom(ctx, "payouts are worked by a staff member")
	if err != nil {
		return nil, err
	}

	payout, err := act(staff)
	if err != nil {
		return nil, h.mapOperationsError(err)
	}

	return &walletv1.PayoutResponse{Payout: toProtoPayout(payout)}, nil
}

func (h *WalletHandler) InspectWallet(
	ctx context.Context,
	request *walletv1.InspectWalletRequest,
) (*walletv1.InspectWalletResponse, error) {
	if h.operations == nil {
		return nil, errOperationsUnavailable
	}

	found, err := h.operations.Inspect(ctx, toDomainOwnerType(request.GetOwnerType()), request.GetOwnerId())
	if err != nil {
		return nil, h.mapOperationsError(err)
	}

	response := &walletv1.InspectWalletResponse{
		Wallet:          toProtoWallet(found.Wallet),
		OutstandingDues: found.OutstandingDues.String(),
	}

	for _, t := range found.Recent {
		response.RecentTransactions = append(response.RecentTransactions, toProtoTransaction(t))
	}

	for _, p := range found.OpenPayouts {
		response.OpenPayouts = append(response.OpenPayouts, toProtoPayout(p))
	}

	return response, nil
}

// GetStatementForStaff is GetStatement for any wallet (wallets.read).
func (h *WalletHandler) GetStatementForStaff(
	ctx context.Context,
	request *walletv1.GetStatementRequest,
) (*walletv1.GetStatementResponse, error) {
	return h.GetStatement(ctx, request)
}

func (h *WalletHandler) AdjustWallet(
	ctx context.Context,
	request *walletv1.AdjustWalletRequest,
) (*walletv1.AdjustmentResponse, error) {
	if h.operations == nil {
		return nil, errOperationsUnavailable
	}

	staff, err := personFrom(ctx, "a wallet is adjusted by a staff member")
	if err != nil {
		return nil, err
	}

	amount, err := parseMoney(request.GetAmount())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, operations.ErrInvalidAmount.Error())
	}

	ownerType := toDomainOwnerType(request.GetOwnerType())

	made, balance, err := h.operations.Adjust(ctx, operations.AdjustInput{
		StaffIdentityID: staff,
		OwnerType:       ownerType,
		OwnerID:         request.GetOwnerId(),
		Amount:          amount,
		Reason:          request.GetReason(),
		IdempotencyKey:  request.GetIdempotencyKey(),
	})
	if err != nil {
		return nil, h.mapOperationsError(err)
	}

	return h.adjustmentResponse(ctx, made, balance)
}

func (h *WalletHandler) RefundTrip(
	ctx context.Context,
	request *walletv1.RefundTripRequest,
) (*walletv1.AdjustmentResponse, error) {
	if h.operations == nil {
		return nil, errOperationsUnavailable
	}

	staff, err := personFrom(ctx, "a trip is refunded by a staff member")
	if err != nil {
		return nil, err
	}

	amount, err := parseMoney(request.GetAmount())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, operations.ErrInvalidRefund.Error())
	}

	driverAmount := decimal.Zero
	if request.GetDriverAmount() != "" {
		if driverAmount, err = parseMoney(request.GetDriverAmount()); err != nil {
			return nil, status.Error(codes.InvalidArgument, operations.ErrInvalidRefund.Error())
		}
	}

	made, balance, err := h.operations.Refund(ctx, operations.RefundInput{
		StaffIdentityID: staff,
		TripID:          request.GetTripId(),
		Amount:          amount,
		DriverAmount:    driverAmount,
		Reason:          request.GetReason(),
		IdempotencyKey:  request.GetIdempotencyKey(),
	})
	if err != nil {
		return nil, h.mapOperationsError(err)
	}

	return h.adjustmentResponse(ctx, made, balance)
}

// adjustmentResponse reads the wallet as it is now for a retried operation
// (the store returns none then).
func (h *WalletHandler) adjustmentResponse(ctx context.Context, made operations.Adjustment, balance wallet.Wallet) (*walletv1.AdjustmentResponse, error) {
	if balance.ID == "" {
		found, err := h.walletService.GetWallet(ctx, made.OwnerType, made.OwnerID)
		if err != nil {
			return nil, h.mapWalletError(err)
		}

		balance = found
	}

	return &walletv1.AdjustmentResponse{Adjustment: toProtoAdjustment(made), Wallet: toProtoWallet(balance)}, nil
}

func (h *WalletHandler) ListTripRefunds(
	ctx context.Context,
	request *walletv1.ListTripRefundsRequest,
) (*walletv1.ListTripRefundsResponse, error) {
	if h.operations == nil {
		return nil, errOperationsUnavailable
	}

	found, err := h.operations.TripRefunds(ctx, request.GetTripId())
	if err != nil {
		return nil, h.mapOperationsError(err)
	}

	response := &walletv1.ListTripRefundsResponse{
		PaidAmount:     found.Charged.String(),
		RefundedAmount: found.Refunded.String(),
	}

	for _, r := range found.Refunds {
		response.Refunds = append(response.Refunds, toProtoAdjustment(r))
	}

	return response, nil
}

func (h *WalletHandler) mapOperationsError(err error) error {
	switch {
	case errors.Is(err, operations.ErrPayoutNotFound),
		errors.Is(err, operations.ErrTripNotSettled):
		return status.Error(codes.NotFound, err.Error())

	case errors.Is(err, operations.ErrKeyReused):
		return status.Error(codes.AlreadyExists, err.Error())

	case errors.Is(err, operations.ErrRefundTooLarge),
		errors.Is(err, operations.ErrPayoutOpen),
		errors.Is(err, operations.ErrPayoutState):
		return status.Error(codes.FailedPrecondition, err.Error())

	case errors.Is(err, operations.ErrOwnerRequired),
		errors.Is(err, operations.ErrTripRequired),
		errors.Is(err, operations.ErrDriverRequired),
		errors.Is(err, operations.ErrInvalidAmount),
		errors.Is(err, operations.ErrInvalidRefund),
		errors.Is(err, operations.ErrReasonRequired),
		errors.Is(err, operations.ErrIdempotencyKey),
		errors.Is(err, operations.ErrDestinationTooLong),
		errors.Is(err, operations.ErrReferenceRequired),
		errors.Is(err, operations.ErrInvalidStatus),
		errors.Is(err, operations.ErrInvalidPageToken),
		errors.Is(err, wallet.ErrInvalidAmount):
		return status.Error(codes.InvalidArgument, err.Error())

	case errors.Is(err, operations.ErrStaffRequired):
		return status.Error(codes.PermissionDenied, err.Error())

	default:
		return h.mapWalletError(err)
	}
}

func toProtoPayout(p operations.Payout) *walletv1.Payout {
	out := &walletv1.Payout{
		Id:            p.ID,
		DriverId:      p.DriverID,
		Amount:        p.Amount.String(),
		CurrencyCode:  p.CurrencyCode,
		Destination:   p.Destination,
		Status:        string(p.Status),
		CreatedAt:     timestamppb.New(p.CreatedAt),
		PaidReference: p.PaidReference,
		RejectReason:  p.RejectReason,
	}

	if p.ApprovedAt != nil {
		out.ApprovedAt = timestamppb.New(*p.ApprovedAt)
	}

	if p.PaidAt != nil {
		out.PaidAt = timestamppb.New(*p.PaidAt)
	}

	if p.RejectedAt != nil {
		out.RejectedAt = timestamppb.New(*p.RejectedAt)
	}

	return out
}

func toProtoPayoutPage(page operations.PayoutPage) *walletv1.ListPayoutsResponse {
	out := &walletv1.ListPayoutsResponse{}

	if page.NextOffset > 0 {
		out.NextPageToken = strconv.Itoa(page.NextOffset)
	}

	for _, p := range page.Payouts {
		out.Payouts = append(out.Payouts, toProtoPayout(p))
	}

	return out
}

func toProtoAdjustment(a operations.Adjustment) *walletv1.Adjustment {
	return &walletv1.Adjustment{
		Id:            a.ID,
		Kind:          string(a.Kind),
		OwnerType:     toProtoOwnerType(a.OwnerType),
		OwnerId:       a.OwnerID,
		Amount:        a.Amount.String(),
		CurrencyCode:  a.CurrencyCode,
		TripId:        a.TripID,
		DriverId:      a.DriverID,
		DriverAmount:  a.DriverAmount.String(),
		Reason:        a.Reason,
		TransactionId: a.TransactionID,
		CreatedBy:     a.CreatedBy,
		CreatedAt:     timestamppb.New(a.CreatedAt),
	}
}

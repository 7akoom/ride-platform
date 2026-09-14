package grpc

import (
	"context"
	"errors"

	walletv1 "github.com/7akoom/ride-platform/gen/go/ride/wallet/v1"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
	"github.com/shopspring/decimal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type WalletHandler struct {
	walletv1.UnimplementedWalletServiceServer

	walletService wallet.Service
}

func NewWalletHandler(walletService wallet.Service) *WalletHandler {
	if walletService == nil {
		panic("wallet service is required")
	}

	return &WalletHandler{walletService: walletService}
}

func (h *WalletHandler) GetWallet(
	ctx context.Context,
	request *walletv1.GetWalletRequest,
) (*walletv1.GetWalletResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	found, err := h.walletService.GetWallet(
		ctx,
		toDomainOwnerType(request.GetOwnerType()),
		request.GetOwnerId(),
	)
	if err != nil {
		return nil, mapWalletError(err)
	}

	return &walletv1.GetWalletResponse{Wallet: toProtoWallet(found)}, nil
}

func (h *WalletHandler) TopUp(
	ctx context.Context,
	request *walletv1.TopUpRequest,
) (*walletv1.TopUpResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	amount, err := parseMoney(request.GetAmount())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "amount must be a valid decimal value")
	}

	updated, transaction, err := h.walletService.TopUp(ctx, wallet.TopUpInput{
		OwnerType:      toDomainOwnerType(request.GetOwnerType()),
		OwnerID:        request.GetOwnerId(),
		Amount:         amount,
		IdempotencyKey: request.GetIdempotencyKey(),
		Description:    request.GetDescription(),
	})
	if err != nil {
		return nil, mapWalletError(err)
	}

	return &walletv1.TopUpResponse{
		Wallet:      toProtoWallet(updated),
		Transaction: toProtoTransaction(transaction),
	}, nil
}

func (h *WalletHandler) SettleTrip(
	ctx context.Context,
	request *walletv1.SettleTripRequest,
) (*walletv1.SettleTripResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	fare, err := parseMoney(request.GetFareAmount())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "fare_amount must be a valid decimal value")
	}

	settlement, err := h.walletService.SettleTrip(ctx, wallet.SettleTripInput{
		TripID:        request.GetTripId(),
		RiderID:       request.GetRiderId(),
		DriverID:      request.GetDriverId(),
		FareAmount:    fare,
		PaymentMethod: toDomainPaymentMethod(request.GetPaymentMethod()),
	})
	if err != nil {
		return nil, mapWalletError(err)
	}

	return &walletv1.SettleTripResponse{
		TripId:           settlement.TripID,
		FareAmount:       settlement.FareAmount.String(),
		CommissionAmount: settlement.CommissionAmount.String(),
		DriverEarning:    settlement.DriverEarning.String(),
		RiderBalance:     settlement.RiderBalance.String(),
		DriverBalance:    settlement.DriverBalance.String(),
	}, nil
}

func (h *WalletHandler) ListTransactions(
	ctx context.Context,
	request *walletv1.ListTransactionsRequest,
) (*walletv1.ListTransactionsResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	transactions, err := h.walletService.ListTransactions(
		ctx,
		toDomainOwnerType(request.GetOwnerType()),
		request.GetOwnerId(),
		int(request.GetLimit()),
	)
	if err != nil {
		return nil, mapWalletError(err)
	}

	protoTransactions := make([]*walletv1.Transaction, len(transactions))

	for i, transaction := range transactions {
		protoTransactions[i] = toProtoTransaction(transaction)
	}

	return &walletv1.ListTransactionsResponse{Transactions: protoTransactions}, nil
}

func (h *WalletHandler) CheckDriverStanding(
	ctx context.Context,
	request *walletv1.CheckDriverStandingRequest,
) (*walletv1.CheckDriverStandingResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	standing, err := h.walletService.CheckDriverStanding(ctx, request.GetDriverId())
	if err != nil {
		return nil, mapWalletError(err)
	}

	return &walletv1.CheckDriverStandingResponse{
		CanTakeTrips:        standing.CanTakeTrips,
		Suspended:           standing.Suspended,
		CurrentBalance:      standing.CurrentBalance.String(),
		SuspensionThreshold: standing.SuspensionThreshold.String(),
		AmountDue:           standing.AmountDue.String(),
		Reason:              standing.Reason,
	}, nil
}

func (h *WalletHandler) RequestPayout(
	ctx context.Context,
	request *walletv1.RequestPayoutRequest,
) (*walletv1.RequestPayoutResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	amount, err := parseMoney(request.GetAmount())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "amount must be a valid decimal value")
	}

	updated, transaction, err := h.walletService.RequestPayout(ctx, wallet.PayoutInput{
		DriverID:       request.GetDriverId(),
		Amount:         amount,
		IdempotencyKey: request.GetIdempotencyKey(),
	})
	if err != nil {
		return nil, mapWalletError(err)
	}

	return &walletv1.RequestPayoutResponse{
		Wallet:      toProtoWallet(updated),
		Transaction: toProtoTransaction(transaction),
	}, nil
}

// parseMoney converts the wire's decimal string into exact decimal.
// Money crosses the wire as a string, never a float — a float64 field
// in protobuf would reintroduce exactly the precision problem the
// NUMERIC columns exist to avoid.
func parseMoney(value string) (wallet.Money, error) {
	if value == "" {
		return decimal.Zero, nil
	}

	return decimal.NewFromString(value)
}

func mapWalletError(err error) error {
	switch {
	case errors.Is(err, wallet.ErrWalletNotFound):
		return status.Error(codes.NotFound, "wallet not found")

	case errors.Is(err, wallet.ErrDuplicateRequest):
		return status.Error(codes.AlreadyExists, "this request was already processed")

	case errors.Is(err, wallet.ErrInsufficientFunds),
		errors.Is(err, wallet.ErrDriverSuspended),
		errors.Is(err, wallet.ErrBelowMinimumPayout),
		errors.Is(err, wallet.ErrWalletBlocked),
		errors.Is(err, wallet.ErrNoActiveConfig):
		return status.Error(codes.FailedPrecondition, err.Error())

	case errors.Is(err, wallet.ErrOwnerIDRequired),
		errors.Is(err, wallet.ErrInvalidOwnerType),
		errors.Is(err, wallet.ErrInvalidPaymentMethod),
		errors.Is(err, wallet.ErrTripIDRequired),
		errors.Is(err, wallet.ErrRiderIDRequired),
		errors.Is(err, wallet.ErrDriverIDRequired),
		errors.Is(err, wallet.ErrInvalidAmount),
		errors.Is(err, wallet.ErrInvalidFareAmount):
		return status.Error(codes.InvalidArgument, err.Error())

	default:
		return status.Error(codes.Internal, "failed to process wallet request")
	}
}

func toDomainOwnerType(o walletv1.OwnerType) wallet.OwnerType {
	switch o {
	case walletv1.OwnerType_OWNER_TYPE_RIDER:
		return wallet.OwnerRider
	case walletv1.OwnerType_OWNER_TYPE_DRIVER:
		return wallet.OwnerDriver
	default:
		return ""
	}
}

func toProtoOwnerType(o wallet.OwnerType) walletv1.OwnerType {
	switch o {
	case wallet.OwnerRider:
		return walletv1.OwnerType_OWNER_TYPE_RIDER
	case wallet.OwnerDriver:
		return walletv1.OwnerType_OWNER_TYPE_DRIVER
	default:
		return walletv1.OwnerType_OWNER_TYPE_UNSPECIFIED
	}
}

func toDomainPaymentMethod(p walletv1.PaymentMethod) wallet.PaymentMethod {
	switch p {
	case walletv1.PaymentMethod_PAYMENT_METHOD_CASH:
		return wallet.PaymentCash
	case walletv1.PaymentMethod_PAYMENT_METHOD_WALLET:
		return wallet.PaymentWallet
	case walletv1.PaymentMethod_PAYMENT_METHOD_CARD:
		return wallet.PaymentCard
	default:
		return ""
	}
}

func toProtoTransactionType(t wallet.TransactionType) walletv1.TransactionType {
	switch t {
	case wallet.TxTopUp:
		return walletv1.TransactionType_TRANSACTION_TYPE_TOP_UP
	case wallet.TxTripPayment:
		return walletv1.TransactionType_TRANSACTION_TYPE_TRIP_PAYMENT
	case wallet.TxTripEarning:
		return walletv1.TransactionType_TRANSACTION_TYPE_TRIP_EARNING
	case wallet.TxCommission:
		return walletv1.TransactionType_TRANSACTION_TYPE_COMMISSION
	case wallet.TxPayout:
		return walletv1.TransactionType_TRANSACTION_TYPE_PAYOUT
	case wallet.TxAdjustment:
		return walletv1.TransactionType_TRANSACTION_TYPE_ADJUSTMENT
	default:
		return walletv1.TransactionType_TRANSACTION_TYPE_UNSPECIFIED
	}
}

func toProtoWallet(w wallet.Wallet) *walletv1.Wallet {
	return &walletv1.Wallet{
		Id:           w.ID,
		OwnerType:    toProtoOwnerType(w.OwnerType),
		OwnerId:      w.OwnerID,
		CurrencyCode: w.CurrencyCode,
		Balance:      w.Balance.String(),
		Blocked:      w.Blocked,
		CreatedAt:    timestamppb.New(w.CreatedAt),
		UpdatedAt:    timestamppb.New(w.UpdatedAt),
	}
}

func toProtoTransaction(t wallet.Transaction) *walletv1.Transaction {
	return &walletv1.Transaction{
		Id:           t.ID,
		WalletId:     t.WalletID,
		Type:         toProtoTransactionType(t.Type),
		Amount:       t.Amount.String(),
		BalanceAfter: t.BalanceAfter.String(),
		TripId:       t.TripID,
		Description:  t.Description,
		CreatedAt:    timestamppb.New(t.CreatedAt),
	}
}

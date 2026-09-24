package grpc

import (
	"context"
	"errors"
	"strconv"
	"time"

	walletv1 "github.com/7akoom/ride-platform/gen/go/ride/wallet/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/transfer"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

// WithStatements gives the handler wallet statements.
func (h *WalletHandler) WithStatements(reader wallet.StatementReader) *WalletHandler {
	h.statements = reader

	return h
}

// personFrom is the identity of the person calling. The internal token is
// no person: it never asks for money or pays.
func personFrom(ctx context.Context, what string) (string, error) {
	principal, ok := authenticatedPrincipalFromContext(ctx)
	if !ok || principal.IdentityID == internalServicePrincipalID {
		return "", status.Error(codes.PermissionDenied, what)
	}

	return principal.IdentityID, nil
}

func (h *WalletHandler) CreateMoneyRequest(
	ctx context.Context,
	request *walletv1.CreateMoneyRequestRequest,
) (*walletv1.MoneyRequestResponse, error) {
	if h.transfers == nil {
		return nil, status.Error(codes.Unimplemented, "money requests are not available")
	}

	identityID, err := personFrom(ctx, "money is asked for by a person")
	if err != nil {
		return nil, err
	}

	amount, err := parseMoney(request.GetAmount())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, transfer.ErrInvalidAmount.Error())
	}

	created, err := h.transfers.CreateRequest(ctx, transfer.RequestInput{
		RequesterRiderID:    request.GetRiderId(),
		RequesterIdentityID: identityID,
		PayerPhone:          request.GetPayerPhone(),
		Amount:              amount,
		Note:                request.GetNote(),
		ExpiresInHours:      int(request.GetExpiresInHours()),
		IdempotencyKey:      request.GetIdempotencyKey(),
	})
	if err != nil {
		return nil, h.mapTransferError(err)
	}

	return &walletv1.MoneyRequestResponse{MoneyRequest: toProtoMoneyRequest(created, request.GetRiderId(), time.Now())}, nil
}

func (h *WalletHandler) ListMoneyRequests(
	ctx context.Context,
	request *walletv1.ListMoneyRequestsRequest,
) (*walletv1.ListMoneyRequestsResponse, error) {
	if h.transfers == nil {
		return nil, status.Error(codes.Unimplemented, "money requests are not available")
	}

	page, err := h.transfers.ListRequests(
		ctx,
		request.GetRiderId(),
		request.GetRole(),
		request.GetStatus(),
		int(request.GetPageSize()),
		request.GetPageToken(),
	)
	if err != nil {
		return nil, h.mapTransferError(err)
	}

	now := time.Now()
	response := &walletv1.ListMoneyRequestsResponse{}

	if page.NextOffset > 0 {
		response.NextPageToken = strconv.Itoa(page.NextOffset)
	}

	for _, r := range page.Requests {
		response.MoneyRequests = append(response.MoneyRequests, toProtoMoneyRequest(r, request.GetRiderId(), now))
	}

	return response, nil
}

func (h *WalletHandler) GetMoneyRequest(
	ctx context.Context,
	request *walletv1.GetMoneyRequestRequest,
) (*walletv1.MoneyRequestResponse, error) {
	if h.transfers == nil {
		return nil, status.Error(codes.Unimplemented, "money requests are not available")
	}

	found, err := h.transfers.GetRequest(ctx, request.GetRiderId(), request.GetCode())
	if err != nil {
		return nil, h.mapTransferError(err)
	}

	return &walletv1.MoneyRequestResponse{MoneyRequest: toProtoMoneyRequest(found, request.GetRiderId(), time.Now())}, nil
}

func (h *WalletHandler) PayMoneyRequest(
	ctx context.Context,
	request *walletv1.PayMoneyRequestRequest,
) (*walletv1.PayMoneyRequestResponse, error) {
	if h.transfers == nil {
		return nil, status.Error(codes.Unimplemented, "money requests are not available")
	}

	identityID, err := personFrom(ctx, "a request is paid by a person, with their PIN")
	if err != nil {
		return nil, err
	}

	paid, sent, payer, err := h.transfers.PayRequest(ctx, transfer.PayInput{
		PayerRiderID:    request.GetRiderId(),
		PayerIdentityID: identityID,
		Code:            request.GetCode(),
		PIN:             request.GetPin(),
	})
	if err != nil {
		return nil, h.mapTransferError(err)
	}

	// A retried payment returns the one made the first time: the wallet is
	// read as it is now.
	if payer.ID == "" {
		if payer, err = h.walletService.GetWallet(ctx, wallet.OwnerRider, request.GetRiderId()); err != nil {
			return nil, h.mapWalletError(err)
		}
	}

	return &walletv1.PayMoneyRequestResponse{
		MoneyRequest: toProtoMoneyRequest(paid, request.GetRiderId(), time.Now()),
		Transfer:     toProtoTransfer(sent, request.GetRiderId()),
		Wallet:       toProtoWallet(payer),
	}, nil
}

func (h *WalletHandler) DeclineMoneyRequest(
	ctx context.Context,
	request *walletv1.CloseMoneyRequestRequest,
) (*walletv1.MoneyRequestResponse, error) {
	if h.transfers == nil {
		return nil, status.Error(codes.Unimplemented, "money requests are not available")
	}

	declined, err := h.transfers.DeclineRequest(ctx, request.GetRiderId(), request.GetCode())
	if err != nil {
		return nil, h.mapTransferError(err)
	}

	return &walletv1.MoneyRequestResponse{MoneyRequest: toProtoMoneyRequest(declined, request.GetRiderId(), time.Now())}, nil
}

func (h *WalletHandler) CancelMoneyRequest(
	ctx context.Context,
	request *walletv1.CloseMoneyRequestRequest,
) (*walletv1.MoneyRequestResponse, error) {
	if h.transfers == nil {
		return nil, status.Error(codes.Unimplemented, "money requests are not available")
	}

	cancelled, err := h.transfers.CancelRequest(ctx, request.GetRiderId(), request.GetCode())
	if err != nil {
		return nil, h.mapTransferError(err)
	}

	return &walletv1.MoneyRequestResponse{MoneyRequest: toProtoMoneyRequest(cancelled, request.GetRiderId(), time.Now())}, nil
}

func (h *WalletHandler) GetStatement(
	ctx context.Context,
	request *walletv1.GetStatementRequest,
) (*walletv1.GetStatementResponse, error) {
	if h.statements == nil {
		return nil, status.Error(codes.Unimplemented, "statements are not available")
	}

	input := wallet.StatementInput{
		OwnerType: toDomainOwnerType(request.GetOwnerType()),
		OwnerID:   request.GetOwnerId(),
		Direction: request.GetDirection(),
		PageSize:  int(request.GetPageSize()),
		PageToken: request.GetPageToken(),
	}

	if request.GetFrom() != nil {
		input.From = request.GetFrom().AsTime()
	}

	if request.GetTo() != nil {
		input.To = request.GetTo().AsTime()
	}

	for _, t := range request.GetTypes() {
		domain, ok := toDomainTransactionType(t)
		if !ok {
			return nil, status.Error(codes.InvalidArgument, wallet.ErrInvalidType.Error())
		}

		input.Types = append(input.Types, domain)
	}

	statement, err := wallet.GetStatement(ctx, h.statements, input, time.Now().UTC())
	if err != nil {
		return nil, h.mapStatementError(err)
	}

	response := &walletv1.GetStatementResponse{
		CurrencyCode:   statement.CurrencyCode,
		OpeningBalance: statement.Opening.String(),
		ClosingBalance: statement.Closing.String(),
		TotalIn:        statement.TotalIn.String(),
		TotalOut:       statement.TotalOut.String(),
	}

	if statement.NextOffset > 0 {
		response.NextPageToken = strconv.Itoa(statement.NextOffset)
	}

	for _, entry := range statement.Entries {
		response.Entries = append(response.Entries, toProtoTransaction(entry))
	}

	return response, nil
}

func (h *WalletHandler) mapStatementError(err error) error {
	switch {
	case errors.Is(err, wallet.ErrInvalidDirection),
		errors.Is(err, wallet.ErrInvalidType),
		errors.Is(err, wallet.ErrInvalidPeriod),
		errors.Is(err, wallet.ErrInvalidPageToken):
		return status.Error(codes.InvalidArgument, err.Error())
	default:
		return h.mapWalletError(err)
	}
}

// GetRiderDues is what a rider still owes from cancelled trips' fees.
// trip-service asks it (with the internal token) before a rider requests a
// trip.
func (h *WalletHandler) GetRiderDues(
	ctx context.Context,
	request *walletv1.GetRiderDuesRequest,
) (*walletv1.GetRiderDuesResponse, error) {
	dues, err := h.walletService.RiderDues(ctx, request.GetRiderId())
	if err != nil {
		return nil, h.mapWalletError(err)
	}

	response := &walletv1.GetRiderDuesResponse{
		CurrencyCode:    dues.CurrencyCode,
		Outstanding:     dues.Outstanding.String(),
		CanRequestTrips: dues.CanRequestTrips,
	}

	for _, due := range dues.Dues {
		response.Dues = append(response.Dues, &walletv1.RiderDue{
			TripId:      due.TripID,
			Kind:        string(due.Kind),
			Amount:      due.Amount.String(),
			Paid:        due.Paid.String(),
			Outstanding: due.Outstanding().String(),
			CreatedAt:   timestamppb.New(due.CreatedAt),
		})
	}

	return response, nil
}

// toProtoMoneyRequest is the request as the viewer sees it. A rider opening
// someone else's open request sees the requester's phone masked, and not who
// paid it.
func toProtoMoneyRequest(r transfer.MoneyRequest, viewer string, now time.Time) *walletv1.MoneyRequest {
	role := r.Role(viewer)

	out := &walletv1.MoneyRequest{
		Id:             r.ID,
		Code:           r.Code,
		Role:           role,
		Open:           r.Open,
		RequesterPhone: r.RequesterPhone,
		PayerPhone:     r.PayerPhone,
		Amount:         r.Amount.String(),
		CurrencyCode:   r.CurrencyCode,
		Note:           r.Note,
		Status:         string(r.StatusAt(now)),
		TransferId:     r.TransferID,
		ExpiresAt:      timestamppb.New(r.ExpiresAt),
		CreatedAt:      timestamppb.New(r.CreatedAt),
	}

	if r.ClosedAt != nil {
		out.ClosedAt = timestamppb.New(*r.ClosedAt)
	}

	if role == "viewer" {
		out.RequesterPhone = transfer.MaskPhone(r.RequesterPhone)
		out.PayerPhone = ""
		out.TransferId = ""
	}

	return out
}

func toDomainTransactionType(t walletv1.TransactionType) (wallet.TransactionType, bool) {
	switch t {
	case walletv1.TransactionType_TRANSACTION_TYPE_TOP_UP:
		return wallet.TxTopUp, true
	case walletv1.TransactionType_TRANSACTION_TYPE_TRIP_PAYMENT:
		return wallet.TxTripPayment, true
	case walletv1.TransactionType_TRANSACTION_TYPE_TRIP_EARNING:
		return wallet.TxTripEarning, true
	case walletv1.TransactionType_TRANSACTION_TYPE_COMMISSION:
		return wallet.TxCommission, true
	case walletv1.TransactionType_TRANSACTION_TYPE_PAYOUT:
		return wallet.TxPayout, true
	case walletv1.TransactionType_TRANSACTION_TYPE_ADJUSTMENT:
		return wallet.TxAdjustment, true
	case walletv1.TransactionType_TRANSACTION_TYPE_CHANGE_CREDIT:
		return wallet.TxChangeCredit, true
	case walletv1.TransactionType_TRANSACTION_TYPE_TRANSFER_OUT:
		return wallet.TxTransferOut, true
	case walletv1.TransactionType_TRANSACTION_TYPE_TRANSFER_IN:
		return wallet.TxTransferIn, true
	case walletv1.TransactionType_TRANSACTION_TYPE_DUE_PAYMENT:
		return wallet.TxDuePayment, true
	default:
		return "", false
	}
}

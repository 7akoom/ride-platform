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

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/voucher"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

// WithVouchers gives the handler vouchers.
func (h *WalletHandler) WithVouchers(vouchers *voucher.Service) *WalletHandler {
	h.vouchers = vouchers

	return h
}

var errVouchersUnavailable = status.Error(codes.Unimplemented, "vouchers are not available")

// staffPersonFrom is the staff member calling. The internal token is nobody:
// it issues, exports, cancels and voids nothing.
func staffPersonFrom(ctx context.Context) (string, error) {
	return personFrom(ctx, "vouchers are managed by a staff member")
}

func (h *WalletHandler) CreateVoucherBatch(
	ctx context.Context,
	request *walletv1.CreateVoucherBatchRequest,
) (*walletv1.VoucherBatchResponse, error) {
	if h.vouchers == nil {
		return nil, errVouchersUnavailable
	}

	staff, err := staffPersonFrom(ctx)
	if err != nil {
		return nil, err
	}

	amount, err := parseMoney(request.GetAmount())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, voucher.ErrInvalidAmount.Error())
	}

	var expires time.Time
	if request.GetExpiresAt() != nil {
		expires = request.GetExpiresAt().AsTime()
	}

	created, err := h.vouchers.CreateBatch(ctx, voucher.CreateInput{
		StaffIdentityID: staff,
		Label:           request.GetLabel(),
		Seller:          request.GetSeller(),
		Amount:          amount,
		Quantity:        int(request.GetQuantity()),
		ExpiresAt:       expires,
		IdempotencyKey:  request.GetIdempotencyKey(),
	})
	if err != nil {
		return nil, h.mapVoucherError(err)
	}

	return &walletv1.VoucherBatchResponse{Batch: toProtoVoucherBatch(created)}, nil
}

func (h *WalletHandler) ListVoucherBatches(
	ctx context.Context,
	request *walletv1.ListVoucherBatchesRequest,
) (*walletv1.ListVoucherBatchesResponse, error) {
	if h.vouchers == nil {
		return nil, errVouchersUnavailable
	}

	page, err := h.vouchers.ListBatches(ctx, request.GetStatus(), int(request.GetPageSize()), request.GetPageToken())
	if err != nil {
		return nil, h.mapVoucherError(err)
	}

	response := &walletv1.ListVoucherBatchesResponse{}

	if page.NextOffset > 0 {
		response.NextPageToken = strconv.Itoa(page.NextOffset)
	}

	for _, b := range page.Batches {
		response.Batches = append(response.Batches, toProtoVoucherBatch(b))
	}

	return response, nil
}

func (h *WalletHandler) GetVoucherBatch(
	ctx context.Context,
	request *walletv1.GetVoucherBatchRequest,
) (*walletv1.VoucherBatchResponse, error) {
	if h.vouchers == nil {
		return nil, errVouchersUnavailable
	}

	found, err := h.vouchers.GetBatch(ctx, request.GetBatchId())
	if err != nil {
		return nil, h.mapVoucherError(err)
	}

	return &walletv1.VoucherBatchResponse{Batch: toProtoVoucherBatch(found)}, nil
}

func (h *WalletHandler) ExportVoucherBatch(
	ctx context.Context,
	request *walletv1.ExportVoucherBatchRequest,
) (*walletv1.ExportVoucherBatchResponse, error) {
	if h.vouchers == nil {
		return nil, errVouchersUnavailable
	}

	staff, err := staffPersonFrom(ctx)
	if err != nil {
		return nil, err
	}

	batch, exported, err := h.vouchers.ExportBatch(ctx, staff, request.GetBatchId())
	if err != nil {
		return nil, h.mapVoucherError(err)
	}

	sheet, err := voucher.CSV(batch, exported)
	if err != nil {
		// The codes are wiped already: the batch has to be cancelled and
		// issued again, so say so loudly.
		h.logger.Error("an exported voucher batch could not be written as CSV", "batch_id", batch.ID, "error", err)
	}

	response := &walletv1.ExportVoucherBatchResponse{Batch: toProtoVoucherBatch(batch), Csv: sheet}

	for _, e := range exported {
		response.Vouchers = append(response.Vouchers, &walletv1.ExportedVoucher{Serial: e.Serial, Code: e.Code})
	}

	return response, nil
}

func (h *WalletHandler) CancelVoucherBatch(
	ctx context.Context,
	request *walletv1.CancelVoucherBatchRequest,
) (*walletv1.VoucherBatchResponse, error) {
	if h.vouchers == nil {
		return nil, errVouchersUnavailable
	}

	staff, err := staffPersonFrom(ctx)
	if err != nil {
		return nil, err
	}

	cancelled, err := h.vouchers.CancelBatch(ctx, staff, request.GetBatchId(), request.GetReason())
	if err != nil {
		return nil, h.mapVoucherError(err)
	}

	return &walletv1.VoucherBatchResponse{Batch: toProtoVoucherBatch(cancelled)}, nil
}

func (h *WalletHandler) GetVoucher(
	ctx context.Context,
	request *walletv1.GetVoucherRequest,
) (*walletv1.VoucherResponse, error) {
	if h.vouchers == nil {
		return nil, errVouchersUnavailable
	}

	found, err := h.vouchers.GetVoucher(ctx, request.GetSerial())
	if err != nil {
		return nil, h.mapVoucherError(err)
	}

	return &walletv1.VoucherResponse{Voucher: toProtoVoucher(found, h.vouchers.Now())}, nil
}

func (h *WalletHandler) VoidVoucher(
	ctx context.Context,
	request *walletv1.VoidVoucherRequest,
) (*walletv1.VoucherResponse, error) {
	if h.vouchers == nil {
		return nil, errVouchersUnavailable
	}

	staff, err := staffPersonFrom(ctx)
	if err != nil {
		return nil, err
	}

	voided, err := h.vouchers.VoidVoucher(ctx, staff, request.GetSerial(), request.GetReason())
	if err != nil {
		return nil, h.mapVoucherError(err)
	}

	return &walletv1.VoucherResponse{Voucher: toProtoVoucher(voided, h.vouchers.Now())}, nil
}

func (h *WalletHandler) RedeemVoucher(
	ctx context.Context,
	request *walletv1.RedeemVoucherRequest,
) (*walletv1.RedeemVoucherResponse, error) {
	if h.vouchers == nil {
		return nil, errVouchersUnavailable
	}

	redeemed, err := h.vouchers.Redeem(ctx, request.GetRiderId(), request.GetCode())
	if err != nil {
		return nil, h.mapVoucherError(err)
	}

	return &walletv1.RedeemVoucherResponse{
		Serial:       redeemed.Serial,
		Amount:       redeemed.Amount.String(),
		CurrencyCode: redeemed.CurrencyCode,
		Wallet:       toProtoWallet(redeemed.Wallet),
		Transaction:  toProtoTransaction(redeemed.Transaction),
	}, nil
}

func (h *WalletHandler) mapVoucherError(err error) error {
	var tooMany *voucher.TooManyAttemptsError

	switch {
	case errors.As(err, &tooMany):
		return status.Error(codes.ResourceExhausted, tooMany.Error())

	case errors.Is(err, voucher.ErrBatchNotFound),
		errors.Is(err, voucher.ErrVoucherNotFound),
		errors.Is(err, voucher.ErrCodeNotValid):
		return status.Error(codes.NotFound, err.Error())

	case errors.Is(err, voucher.ErrKeyReused):
		return status.Error(codes.AlreadyExists, err.Error())

	case errors.Is(err, voucher.ErrNotExportable),
		errors.Is(err, voucher.ErrNotCancellable),
		errors.Is(err, voucher.ErrNotVoidable),
		errors.Is(err, voucher.ErrCodeUsed),
		errors.Is(err, voucher.ErrCodeCancelled),
		errors.Is(err, voucher.ErrCodeExpired):
		return status.Error(codes.FailedPrecondition, err.Error())

	case errors.Is(err, voucher.ErrRiderRequired),
		errors.Is(err, voucher.ErrLabelRequired),
		errors.Is(err, voucher.ErrSellerTooLong),
		errors.Is(err, voucher.ErrInvalidAmount),
		errors.Is(err, voucher.ErrInvalidQuantity),
		errors.Is(err, voucher.ErrInvalidExpiry),
		errors.Is(err, voucher.ErrIdempotencyKey),
		errors.Is(err, voucher.ErrReasonRequired),
		errors.Is(err, voucher.ErrInvalidStatusFilter),
		errors.Is(err, voucher.ErrInvalidPageToken),
		errors.Is(err, voucher.ErrInvalidCode):
		return status.Error(codes.InvalidArgument, err.Error())

	case errors.Is(err, voucher.ErrStaffRequired):
		return status.Error(codes.PermissionDenied, err.Error())

	case errors.Is(err, wallet.ErrDuplicateRequest):
		return status.Error(codes.AlreadyExists, err.Error())

	default:
		h.logger.Error("unclassified voucher failure", "error", err)

		return status.Error(codes.Internal, "the voucher could not be processed")
	}
}

func toProtoVoucherBatch(b voucher.Batch) *walletv1.VoucherBatch {
	out := &walletv1.VoucherBatch{
		Id:             b.ID,
		Number:         b.Number,
		Label:          b.Label,
		Seller:         b.Seller,
		Amount:         b.Amount.String(),
		CurrencyCode:   b.CurrencyCode,
		Quantity:       int32(b.Quantity),
		Status:         string(b.Status),
		ExpiresAt:      timestamppb.New(b.ExpiresAt),
		CreatedAt:      timestamppb.New(b.CreatedAt),
		CancelReason:   b.CancelReason,
		RedeemedCount:  int32(b.RedeemedCount),
		VoidCount:      int32(b.VoidCount),
		RedeemedAmount: b.RedeemedAmount.String(),
	}

	if b.ExportedAt != nil {
		out.ExportedAt = timestamppb.New(*b.ExportedAt)
	}

	if b.CancelledAt != nil {
		out.CancelledAt = timestamppb.New(*b.CancelledAt)
	}

	return out
}

func toProtoVoucher(v voucher.Voucher, now time.Time) *walletv1.Voucher {
	out := &walletv1.Voucher{
		Serial:            v.Serial,
		BatchId:           v.BatchID,
		BatchLabel:        v.BatchLabel,
		Amount:            v.Amount.String(),
		CurrencyCode:      v.CurrencyCode,
		Status:            string(v.Status),
		Redeemable:        v.RedeemableAt(now),
		ExpiresAt:         timestamppb.New(v.ExpiresAt),
		RedeemedByRiderId: v.RedeemedByRiderID,
		TransactionId:     v.TransactionID,
		VoidReason:        v.VoidReason,
	}

	if v.RedeemedAt != nil {
		out.RedeemedAt = timestamppb.New(*v.RedeemedAt)
	}

	if v.VoidedAt != nil {
		out.VoidedAt = timestamppb.New(*v.VoidedAt)
	}

	return out
}

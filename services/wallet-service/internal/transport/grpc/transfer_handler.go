package grpc

import (
	"context"
	"errors"
	"strconv"

	walletv1 "github.com/7akoom/ride-platform/gen/go/ride/wallet/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/transfer"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

// WithTransfers gives the handler rider-to-rider transfers.
func (h *WalletHandler) WithTransfers(transfers *transfer.Service) *WalletHandler {
	h.transfers = transfers

	return h
}

func (h *WalletHandler) SendTransfer(
	ctx context.Context,
	request *walletv1.SendTransferRequest,
) (*walletv1.SendTransferResponse, error) {
	if h.transfers == nil {
		return nil, status.Error(codes.Unimplemented, "transfers are not available")
	}

	// A transfer is confirmed with a person's PIN: the internal token, which
	// is no person, never sends one.
	principal, ok := authenticatedPrincipalFromContext(ctx)
	if !ok || principal.IdentityID == internalServicePrincipalID {
		return nil, status.Error(codes.PermissionDenied, "a transfer is sent by a person, with their PIN")
	}

	amount, err := parseMoney(request.GetAmount())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, transfer.ErrInvalidAmount.Error())
	}

	sent, sender, err := h.transfers.Send(ctx, transfer.SendInput{
		SenderRiderID:    request.GetRiderId(),
		SenderIdentityID: principal.IdentityID,
		RecipientPhone:   request.GetRecipientPhone(),
		Amount:           amount,
		Note:             request.GetNote(),
		PIN:              request.GetPin(),
		IdempotencyKey:   request.GetIdempotencyKey(),
	})
	if err != nil {
		return nil, h.mapTransferError(err)
	}

	// A retried send returns the transfer made the first time: the wallet is
	// read as it is now.
	if sender.ID == "" {
		if sender, err = h.walletService.GetWallet(ctx, wallet.OwnerRider, request.GetRiderId()); err != nil {
			return nil, h.mapWalletError(err)
		}
	}

	return &walletv1.SendTransferResponse{
		Transfer: toProtoTransfer(sent, request.GetRiderId()),
		Wallet:   toProtoWallet(sender),
	}, nil
}

func (h *WalletHandler) ListTransfers(
	ctx context.Context,
	request *walletv1.ListTransfersRequest,
) (*walletv1.ListTransfersResponse, error) {
	if h.transfers == nil {
		return nil, status.Error(codes.Unimplemented, "transfers are not available")
	}

	page, err := h.transfers.List(ctx, request.GetRiderId(), int(request.GetPageSize()), request.GetPageToken())
	if err != nil {
		return nil, h.mapTransferError(err)
	}

	response := &walletv1.ListTransfersResponse{}
	if page.NextOffset > 0 {
		response.NextPageToken = strconv.Itoa(page.NextOffset)
	}

	for _, t := range page.Transfers {
		response.Transfers = append(response.Transfers, toProtoTransfer(t, request.GetRiderId()))
	}

	return response, nil
}

func (h *WalletHandler) mapTransferError(err error) error {
	var (
		wrong  *transfer.WrongPINError
		locked *transfer.LockedPINError
	)

	switch {
	case errors.As(err, &wrong):
		return status.Error(codes.PermissionDenied, wrong.Error())

	case errors.As(err, &locked):
		return status.Error(codes.FailedPrecondition, locked.Error())

	case errors.Is(err, transfer.ErrRecipientNotFound):
		return status.Error(codes.NotFound, err.Error())

	case errors.Is(err, transfer.ErrKeyReused):
		return status.Error(codes.AlreadyExists, err.Error())

	case errors.Is(err, transfer.ErrPINNotSet),
		errors.Is(err, transfer.ErrDailyLimit),
		errors.Is(err, transfer.ErrDailyCount),
		errors.Is(err, wallet.ErrInsufficientFunds),
		errors.Is(err, wallet.ErrWalletBlocked):
		return status.Error(codes.FailedPrecondition, err.Error())

	case errors.Is(err, transfer.ErrSenderRequired),
		errors.Is(err, transfer.ErrIdempotencyKeyNeeded),
		errors.Is(err, transfer.ErrInvalidAmount),
		errors.Is(err, transfer.ErrNoteTooLong),
		errors.Is(err, transfer.ErrInvalidPhone),
		errors.Is(err, transfer.ErrToSelf),
		errors.Is(err, transfer.ErrBelowMin),
		errors.Is(err, transfer.ErrAboveMax),
		errors.Is(err, transfer.ErrInvalidPageToken):
		return status.Error(codes.InvalidArgument, err.Error())

	case errors.Is(err, transfer.ErrIdentityRequired):
		return status.Error(codes.PermissionDenied, err.Error())

	case upstreamUnavailable(err):
		h.logger.Warn("identity-service or rider-service is unavailable", "error", err)

		return status.Error(codes.Unavailable, "transfers are unavailable right now; try again")

	default:
		h.logger.Error("unclassified transfer failure", "error", err)

		return status.Error(codes.Internal, "failed to process the transfer")
	}
}

// upstreamUnavailable reports an error from another service being down or
// slow: the caller may retry (nothing moved).
func upstreamUnavailable(err error) bool {
	for current := err; current != nil; current = errors.Unwrap(current) {
		switch status.Code(current) {
		case codes.Unavailable, codes.DeadlineExceeded:
			return true
		}
	}

	return false
}

func toProtoTransfer(t transfer.Transfer, viewer string) *walletv1.Transfer {
	direction, counterpart := t.Seen(viewer)

	return &walletv1.Transfer{
		Id:               t.ID,
		Direction:        string(direction),
		CounterpartPhone: counterpart,
		Amount:           t.Amount.String(),
		CurrencyCode:     t.CurrencyCode,
		Note:             t.Note,
		CreatedAt:        timestamppb.New(t.CreatedAt),
	}
}

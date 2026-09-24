package grpc

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	walletv1 "github.com/7akoom/ride-platform/gen/go/ride/wallet/v1"
	"github.com/shopspring/decimal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/transfer"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

func TestTheInternalTokenNeverAsksForOrPaysMoney(t *testing.T) {
	handler := &WalletHandler{transfers: &transfer.Service{}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	internal := contextWithAuthenticatedPrincipal(context.Background(), authenticatedPrincipal{IdentityID: internalServicePrincipalID, SessionID: internalServicePrincipalID})

	for _, ctx := range []context.Context{context.Background(), internal} {
		if _, err := handler.CreateMoneyRequest(ctx, &walletv1.CreateMoneyRequestRequest{RiderId: "rider-a", Amount: "1000"}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("create: got %v", err)
		}

		if _, err := handler.PayMoneyRequest(ctx, &walletv1.PayMoneyRequestRequest{RiderId: "rider-a", Code: "ABC"}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("pay: got %v", err)
		}
	}
}

func TestRequestErrorsReachTheCallerAsTheyShould(t *testing.T) {
	handler := &WalletHandler{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

	for err, want := range map[error]codes.Code{
		transfer.ErrRequestNotFound:     codes.NotFound,
		transfer.ErrNotYourRequest:      codes.PermissionDenied,
		transfer.ErrRequestNotPending:   codes.FailedPrecondition,
		transfer.ErrRequestExpired:      codes.FailedPrecondition,
		transfer.ErrOwnRequest:          codes.FailedPrecondition,
		transfer.ErrInvalidExpiry:       codes.InvalidArgument,
		transfer.ErrInvalidRequestRole:  codes.InvalidArgument,
		transfer.ErrInvalidStatusFilter: codes.InvalidArgument,
	} {
		if got := status.Code(handler.mapTransferError(err)); got != want {
			t.Fatalf("%v: got %v, want %v", err, got, want)
		}
	}

	for err, want := range map[error]codes.Code{
		wallet.ErrInvalidPeriod:    codes.InvalidArgument,
		wallet.ErrInvalidDirection: codes.InvalidArgument,
		wallet.ErrWalletNotFound:   codes.NotFound,
	} {
		if got := status.Code(handler.mapStatementError(err)); got != want {
			t.Fatalf("%v: got %v, want %v", err, got, want)
		}
	}
}

func TestAStrangerSeesAnOpenRequestMasked(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	request := transfer.MoneyRequest{
		ID: "request-1", Code: "ABCD234XYZ", RequesterRiderID: "rider-a", RequesterPhone: "+9647701234567",
		PayerRiderID: "rider-b", PayerPhone: "+9647509876543", Open: true, Amount: decimal.NewFromInt(2500),
		Status: transfer.RequestPaid, TransferID: "transfer-1", ExpiresAt: now.Add(time.Hour),
	}

	stranger := toProtoMoneyRequest(request, "rider-c", now)
	if stranger.GetRole() != "viewer" || stranger.GetRequesterPhone() != "+964770***4567" ||
		stranger.GetPayerPhone() != "" || stranger.GetTransferId() != "" {
		t.Fatalf("stranger sees %+v", stranger)
	}

	payer := toProtoMoneyRequest(request, "rider-b", now)
	if payer.GetRole() != "payer" || payer.GetRequesterPhone() != "+9647701234567" || payer.GetTransferId() != "transfer-1" {
		t.Fatalf("payer sees %+v", payer)
	}

	request.Status = transfer.RequestPending
	if got := toProtoMoneyRequest(request, "rider-a", now.Add(2*time.Hour)).GetStatus(); got != "expired" {
		t.Fatalf("status %q", got)
	}
}

func TestEveryTransactionTypeCrossesTheWireBothWays(t *testing.T) {
	for _, value := range walletv1.TransactionType_value {
		proto := walletv1.TransactionType(value)
		if proto == walletv1.TransactionType_TRANSACTION_TYPE_UNSPECIFIED {
			continue
		}

		domain, ok := toDomainTransactionType(proto)
		if !ok {
			t.Fatalf("%v has no domain type", proto)
		}

		if back := transactionTypeForProto(domain); back != proto {
			t.Fatalf("%v -> %s -> %v", proto, domain, back)
		}
	}
}

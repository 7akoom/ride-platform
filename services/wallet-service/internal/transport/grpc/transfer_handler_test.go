package grpc

import (
	"context"
	"io"
	"log/slog"
	"testing"

	walletv1 "github.com/7akoom/ride-platform/gen/go/ride/wallet/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/transfer"
)

func TestTheInternalTokenNeverSendsATransfer(t *testing.T) {
	handler := &WalletHandler{transfers: &transfer.Service{}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

	for _, ctx := range []context.Context{
		context.Background(),
		contextWithAuthenticatedPrincipal(context.Background(), authenticatedPrincipal{IdentityID: internalServicePrincipalID, SessionID: internalServicePrincipalID}),
	} {
		_, err := handler.SendTransfer(ctx, &walletv1.SendTransferRequest{RiderId: "rider-a", Amount: "1000"})
		if status.Code(err) != codes.PermissionDenied {
			t.Fatalf("got %v", err)
		}
	}
}

func TestTransferErrorsReachTheCallerAsTheyShould(t *testing.T) {
	handler := &WalletHandler{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

	for err, want := range map[error]codes.Code{
		&transfer.WrongPINError{AttemptsLeft: 2}: codes.PermissionDenied,
		&transfer.LockedPINError{}:               codes.FailedPrecondition,
		transfer.ErrRecipientNotFound:            codes.NotFound,
		transfer.ErrKeyReused:                    codes.AlreadyExists,
		transfer.ErrDailyLimit:                   codes.FailedPrecondition,
		transfer.ErrPINNotSet:                    codes.FailedPrecondition,
		transfer.ErrToSelf:                       codes.InvalidArgument,
		status.Error(codes.Unavailable, "down"):  codes.Unavailable,
	} {
		if got := status.Code(handler.mapTransferError(err)); got != want {
			t.Fatalf("%v: got %v, want %v", err, got, want)
		}
	}

	if msg := status.Convert(handler.mapTransferError(&transfer.WrongPINError{AttemptsLeft: 2})).Message(); msg != "wrong PIN: 2 attempts left before it locks" {
		t.Fatalf("message %q", msg)
	}
}

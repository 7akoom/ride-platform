package grpc

import (
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/topup"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type loggerTestWalletService struct{ wallet.Service }

type loggerTestTopUpService struct{ topup.Service }

func TestUnclassifiedErrorsAreLoggedNotPanicked(t *testing.T) {
	handler := NewWalletHandler(
		loggerTestWalletService{},
		loggerTestTopUpService{},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)

	if code := status.Code(handler.mapWalletError(errors.New("unexpected"))); code != codes.Internal {
		t.Fatalf("wallet: expected Internal, got %v", code)
	}

	if code := status.Code(handler.mapTopUpError(errors.New("unexpected"))); code != codes.Internal {
		t.Fatalf("top-up: expected Internal, got %v", code)
	}
}

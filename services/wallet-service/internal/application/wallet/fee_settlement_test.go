package wallet_test

import (
	"context"
	"errors"
	"testing"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

func TestAFeeIsTakenFromTheWalletWhateverThePaymentMethod(t *testing.T) {
	for _, kind := range []wallet.SettlementKind{wallet.SettlementCancellation, wallet.SettlementNoShow} {
		repo := newFakeRepository()
		repo.config = defaultConfig() // 20% commission

		input := validSettleInput()
		input.FareAmount = d("1000")
		input.PaymentMethod = wallet.PaymentCash
		input.Kind = kind

		if _, err := newService(repo).SettleTrip(context.Background(), input); err != nil {
			t.Fatal(err)
		}

		call := repo.settleTripCalls[0]
		if call.Kind != kind || call.PaymentMethod != wallet.PaymentWallet ||
			!call.CommissionAmount.Equal(d("200")) || !call.DriverEarning.Equal(d("800")) {
			t.Fatalf("%s: %+v", kind, call)
		}
	}
}

func TestSettlementKinds(t *testing.T) {
	repo := newFakeRepository()
	repo.config = defaultConfig()

	input := validSettleInput()
	if _, err := newService(repo).SettleTrip(context.Background(), input); err != nil || repo.settleTripCalls[0].Kind != wallet.SettlementTrip {
		t.Fatalf("an unnamed kind is a trip: %v %+v", err, repo.settleTripCalls)
	}

	input.Kind = "refund"
	if _, err := newService(repo).SettleTrip(context.Background(), input); !errors.Is(err, wallet.ErrInvalidSettlementKind) {
		t.Fatalf("unknown kind: %v", err)
	}
}

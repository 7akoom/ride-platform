package wallet_test

import (
	"testing"

	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

func splitDec(t *testing.T, s string) decimal.Decimal {
	t.Helper()

	value, err := decimal.NewFromString(s)
	if err != nil {
		t.Fatalf("bad decimal %q: %v", s, err)
	}

	return value
}

func TestSplitWalletPayment(t *testing.T) {
	cases := []struct {
		name       string
		fare       string
		balance    string
		wantWallet string
		wantCash   string
	}{
		{"wallet covers the fare", "5750", "10000", "5750", "0"},
		{"wallet exactly equals the fare", "5750", "5750", "5750", "0"},
		{"wallet covers part of the fare", "5750", "2000", "2000", "3750"},
		{"wallet is empty", "5750", "0", "0", "5750"},
		{"a negative balance counts as empty", "5750", "-100", "0", "5750"},
		{"fractional amounts stay exact", "5750.500", "0.250", "0.250", "5750.250"},
		{"a zero fare has nothing to pay", "0", "1000", "0", "0"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fare := splitDec(t, tc.fare)
			balance := splitDec(t, tc.balance)

			walletPart, cashPart := wallet.SplitWalletPayment(fare, balance)

			if !walletPart.Equal(splitDec(t, tc.wantWallet)) {
				t.Errorf("wallet part = %s, want %s", walletPart, tc.wantWallet)
			}

			if !cashPart.Equal(splitDec(t, tc.wantCash)) {
				t.Errorf("cash part = %s, want %s", cashPart, tc.wantCash)
			}

			if !walletPart.Add(cashPart).Equal(fare) {
				t.Errorf("wallet part %s + cash part %s != fare %s", walletPart, cashPart, fare)
			}

			if balance.IsPositive() && walletPart.GreaterThan(balance) {
				t.Errorf("wallet part %s is more than the balance %s", walletPart, balance)
			}
		})
	}
}

func TestDriverSettlementAmount(t *testing.T) {
	// 20% commission on a 5750 fare is 1150.
	commission := splitDec(t, "1150")
	fare := splitDec(t, "5750")

	cases := []struct {
		name       string
		walletPart string
		want       string
	}{
		{"all wallet: the driver is credited the fare minus the commission", "5750", "4600"},
		{"most of it wallet: still a credit", "2000", "850"},
		{"a wallet part smaller than the commission is a debit", "1000", "-150"},
		{"all cash: the commission is drawn from the driver's balance", "0", "-1150"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			walletPart := splitDec(t, tc.walletPart)

			got := wallet.DriverSettlementAmount(walletPart, commission)

			if !got.Equal(splitDec(t, tc.want)) {
				t.Errorf("driver movement = %s, want %s", got, tc.want)
			}

			// The driver ends the trip with the cash in hand plus the movement,
			// and that must always be the fare minus the commission.
			cash := fare.Sub(walletPart)
			if !cash.Add(got).Equal(fare.Sub(commission)) {
				t.Errorf("cash %s + movement %s != fare - commission %s", cash, got, fare.Sub(commission))
			}
		})
	}
}

package zaincash

import (
	"context"
	"errors"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/topup"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

func signed(t *testing.T, secret, status string) string {
	t.Helper()

	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, WebhookClaims{
		TransactionID:       "zc-1",
		MerchantReferenceID: "ref-1",
		CurrentStatus:       status,
	}).SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}

	return token
}

func TestZainCashStatusesBecomeOutcomes(t *testing.T) {
	adapter := NewAdapter(NewClient(Config{WebhookSecret: "webhook-secret"}))

	for status, want := range map[string]topup.Outcome{
		"SUCCESS":  topup.OutcomeSucceeded,
		"FAILED":   topup.OutcomeFailed,
		"PENDING":  topup.OutcomePending,
		"OTP_SENT": topup.OutcomePending,
		"EXPIRED":  topup.OutcomePending,
	} {
		notice, err := adapter.Verify(signed(t, "webhook-secret", status))
		if err != nil || notice.Outcome != want || notice.ReferenceID != "ref-1" || notice.ProviderTransactionID != "zc-1" {
			t.Errorf("%s: %+v %v", status, notice, err)
		}
	}

	if _, err := adapter.Verify(signed(t, "someone-else", "SUCCESS")); err == nil {
		t.Fatal("a token signed with another secret was accepted")
	}
}

func TestZainCashTakesWholeDinarsOnly(t *testing.T) {
	adapter := NewAdapter(NewClient(Config{}))

	_, err := adapter.Start(context.Background(), topup.StartInput{
		OwnerType: wallet.OwnerRider, Amount: decimal.RequireFromString("1000.5"),
	})
	if !errors.Is(err, topup.ErrAmountNotSupported) {
		t.Fatalf("a fraction: %v", err)
	}
}

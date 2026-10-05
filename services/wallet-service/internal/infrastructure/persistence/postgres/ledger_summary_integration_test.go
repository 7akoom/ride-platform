package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

func TestLedgerSummaryByDayAndType(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewWalletRepository(pool)
	var rider, driver string
	if err := pool.QueryRow(ctx, `SELECT gen_random_uuid()::text, gen_random_uuid()::text`).Scan(&rider, &driver); err != nil {
		t.Fatal(err)
	}

	move := func(owner wallet.OwnerType, id string, kind wallet.TransactionType, amount int64) {
		t.Helper()

		if _, _, err := repo.ApplyMovement(ctx, wallet.MovementInput{
			OwnerType: owner, OwnerID: id, Type: kind, Amount: decimal.NewFromInt(amount), AllowNegative: owner == wallet.OwnerDriver,
		}); err != nil {
			t.Fatal(err)
		}
	}

	move(wallet.OwnerRider, rider, wallet.TxTopUp, 5000)
	move(wallet.OwnerRider, rider, wallet.TxTopUp, 2000)
	move(wallet.OwnerRider, rider, wallet.TxTripPayment, -3000)
	move(wallet.OwnerDriver, driver, wallet.TxCommission, -700)

	now := time.Now().UTC()
	summary, err := NewLedgerSummaries(pool).Summarize(ctx, now.Add(-48*time.Hour), now.Add(time.Hour), "Asia/Baghdad")
	if err != nil {
		t.Fatal(err)
	}

	byKey := map[string]wallet.LedgerTotal{}
	for _, total := range summary.Totals {
		byKey[total.OwnerType+"/"+total.Type] = total
	}

	if top := byKey["rider/top_up"]; top.Entries != 2 || top.Credited.String() != "7000" || !top.Debited.IsZero() {
		t.Fatalf("top-ups %+v (all %+v)", top, summary.Totals)
	}

	if pay := byKey["rider/trip_payment"]; pay.Debited.String() != "3000" || !pay.Credited.IsZero() {
		t.Fatalf("payments %+v", pay)
	}

	if summary.RiderBalances.String() != "4000" || summary.DriverDebt.String() != "700" || !summary.DriverCredit.IsZero() ||
		!summary.RiderDues.IsZero() || summary.CurrencyCode == "" {
		t.Fatalf("balances %+v", summary)
	}

	if _, err := NewLedgerSummaries(pool).Summarize(ctx, now.Add(-time.Hour), now, "Asia/Baghdad"); err != nil {
		t.Fatal(err)
	}
}

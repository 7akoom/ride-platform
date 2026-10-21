package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/operations"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

func TestCashPaidAtTheOfficeIsCreditedOncePerReceipt(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	store := NewOperationsStore(NewWalletRepository(pool))
	service := operations.NewService(store, wallet.NewService(NewWalletRepository(pool)))

	made, balance, err := service.CashTopUp(ctx, operations.CashTopUpInput{
		StaffIdentityID: staffS, OwnerType: wallet.OwnerDriver, OwnerID: driverD,
		Amount: dec(25000), ReceiptNumber: "R-1001", Note: "paid at the counter", IdempotencyKey: "k1",
	})
	if err != nil {
		t.Fatal(err)
	}

	if made.Kind != operations.KindCashTopUp || made.ReceiptNumber != "R-1001" || made.Reason != "paid at the counter" || !balance.Balance.Equal(dec(25000)) {
		t.Fatalf("made %+v, balance %v", made, balance.Balance)
	}

	var txType, description string
	if err := pool.QueryRow(ctx, `SELECT type, description FROM wallet_transactions WHERE id = $1`, made.TransactionID).Scan(&txType, &description); err != nil {
		t.Fatal(err)
	}

	if txType != "cash_top_up" || description != "Cash top-up, receipt R-1001" {
		t.Fatalf("ledger row %q %q", txType, description)
	}

	// The same request again is the same top-up; the same receipt with another
	// key is refused.
	again, _, err := service.CashTopUp(ctx, operations.CashTopUpInput{
		StaffIdentityID: staffS, OwnerType: wallet.OwnerDriver, OwnerID: driverD,
		Amount: dec(25000), ReceiptNumber: "R-1001", IdempotencyKey: "k1",
	})
	if err != nil || again.ID != made.ID {
		t.Fatalf("retry: %+v, %v", again, err)
	}

	if _, _, err := service.CashTopUp(ctx, operations.CashTopUpInput{
		StaffIdentityID: staffS, OwnerType: wallet.OwnerRider, OwnerID: riderA,
		Amount: dec(1000), ReceiptNumber: "R-1001", IdempotencyKey: "k2",
	}); !errors.Is(err, operations.ErrReceiptUsed) {
		t.Fatalf("a receipt used twice: %v", err)
	}

	events := outboxPayloads(t, pool, EventToppedUp)
	if len(events) != 1 || events[0]["provider"] != "office" || events[0]["owner_id"] != driverD || events[0]["balance"] != "25000" {
		t.Fatalf("events %v", events)
	}

	listed, err := service.ListCashTopUps(ctx, time.Time{}, time.Time{}, "")
	if err != nil || listed.Count != 1 || !listed.Total.Equal(dec(25000)) || len(listed.TopUps) != 1 || listed.TopUps[0].ReceiptNumber != "R-1001" {
		t.Fatalf("listed %+v, %v", listed, err)
	}

	if other, _ := service.ListCashTopUps(ctx, time.Time{}, time.Time{}, "00000000-0000-4000-8000-000000000000"); other.Count != 0 {
		t.Fatalf("another staff member's: %+v", other)
	}
}

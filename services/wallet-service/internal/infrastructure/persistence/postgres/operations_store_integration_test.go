package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/operations"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

const tripThree = "33333333-3333-4333-8333-333333333333"

func driverBalance(t *testing.T, repo *WalletRepository, driverID string) (string, bool) {
	t.Helper()

	w, err := repo.FindWallet(context.Background(), wallet.OwnerDriver, driverID)
	if err != nil {
		t.Fatal(err)
	}

	return w.Balance.String(), w.Blocked
}

// settleWalletTrip settles a wallet-paid trip of the fare for riderA and driverD.
func settleWalletTrip(t *testing.T, repo *WalletRepository, tripID string, fare int64) {
	t.Helper()

	if _, err := repo.SettleTrip(context.Background(), wallet.SettleInput{
		TripID: tripID, RiderID: riderA, DriverID: driverD, CurrencyCode: "IQD",
		PaymentMethod: wallet.PaymentWallet, FareAmount: dec(fare), CommissionRate: decimal.RequireFromString("0.2"),
		CommissionAmount: dec(fare / 5), DriverEarning: dec(fare - fare/5), SuspensionFloor: dec(-50000),
		Kind: wallet.SettlementTrip,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestAdjustmentsMoveMoneyWithTheirReason(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewWalletRepository(pool)
	store := NewOperationsStore(repo)

	made, rider, err := store.Adjust(ctx, operations.AdjustRecord{
		OwnerType: wallet.OwnerRider, OwnerID: riderA, Amount: dec(3000), Reason: "goodwill", CreatedBy: staffS, IdempotencyKey: "a1",
	})
	if err != nil || rider.Balance.String() != "3000" || made.Kind != operations.KindAdjustment || made.CurrencyCode != "IQD" || made.TransactionID == "" {
		t.Fatalf("credit %+v %+v %v", made, rider, err)
	}

	rows, _ := repo.ListTransactions(ctx, wallet.OwnerRider, riderA, 5)
	if len(rows) != 1 || rows[0].Type != wallet.TxAdjustment || rows[0].Description != "Adjustment: goodwill" {
		t.Fatalf("ledger %+v", rows)
	}

	if _, _, err := store.Adjust(ctx, operations.AdjustRecord{
		OwnerType: wallet.OwnerRider, OwnerID: riderA, Amount: dec(-5000), Reason: "too much", CreatedBy: staffS, IdempotencyKey: "a2",
	}); !errors.Is(err, wallet.ErrInsufficientFunds) {
		t.Fatalf("a rider below zero: %v", err)
	}

	if _, _, err := store.Adjust(ctx, operations.AdjustRecord{
		OwnerType: wallet.OwnerRider, OwnerID: riderA, Amount: dec(1), Reason: "again", CreatedBy: staffS, IdempotencyKey: "a1",
	}); !errors.Is(err, wallet.ErrDuplicateRequest) {
		t.Fatalf("the same key: %v", err)
	}

	if got := balance(t, repo, riderA); got != "3000" {
		t.Fatalf("refused adjustments moved money: %s", got)
	}

	if found, ok, err := store.FindAdjustmentByKey(ctx, staffS, "a1"); err != nil || !ok || found.ID != made.ID {
		t.Fatalf("by key %+v %v %v", found, ok, err)
	}

	// A driver's commission balance may go below zero, and past the floor
	// they are suspended; a credit lifts them again.
	if _, driver, err := store.Adjust(ctx, operations.AdjustRecord{
		OwnerType: wallet.OwnerDriver, OwnerID: driverD, Amount: dec(-60000), Reason: "commission owed", CreatedBy: staffS, IdempotencyKey: "a3",
	}); err != nil || driver.Balance.String() != "-60000" || !driver.Blocked {
		t.Fatalf("a driver below the floor %+v %v", driver, err)
	}

	if _, driver, err := store.Adjust(ctx, operations.AdjustRecord{
		OwnerType: wallet.OwnerDriver, OwnerID: driverD, Amount: dec(20000), Reason: "paid at the office", CreatedBy: staffS, IdempotencyKey: "a4",
	}); err != nil || driver.Blocked {
		t.Fatalf("a driver back above it %+v %v", driver, err)
	}
}

func TestRefundsNeverPassTheFareAndMayChargeTheDriver(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewWalletRepository(pool)
	store := NewOperationsStore(repo)

	refund := func(key string, amount, driverAmount int64) (operations.Adjustment, wallet.Wallet, error) {
		return store.Refund(ctx, operations.RefundRecord{
			TripID: tripOne, Amount: dec(amount), DriverAmount: dec(driverAmount), Reason: "wrong route", CreatedBy: staffS, IdempotencyKey: key,
		})
	}

	if _, _, err := refund("r0", 100, 0); !errors.Is(err, operations.ErrTripNotSettled) {
		t.Fatalf("an unsettled trip: %v", err)
	}

	fund(t, repo, riderA, 10000)
	settleWalletTrip(t, repo, tripOne, 5000)

	made, rider, err := refund("r1", 3000, 1000)
	if err != nil || rider.Balance.String() != "8000" || made.DriverID != driverD || !made.DriverAmount.Equal(dec(1000)) {
		t.Fatalf("refund %+v %+v %v", made, rider, err)
	}

	// The driver earned 4000 and gave 1000 back.
	if got, _ := driverBalance(t, repo, driverD); got != "3000" {
		t.Fatalf("driver %s", got)
	}

	if _, _, err := refund("r2", 2001, 0); !errors.Is(err, operations.ErrRefundTooLarge) {
		t.Fatalf("past the fare: %v", err)
	}

	if _, _, err := refund("r3", 2000, 0); err != nil {
		t.Fatalf("up to the fare: %v", err)
	}

	listed, err := store.TripRefunds(ctx, tripOne)
	if err != nil || len(listed.Refunds) != 2 || !listed.Charged.Equal(dec(5000)) || !listed.Refunded.Equal(dec(5000)) {
		t.Fatalf("refunds %+v %v", listed, err)
	}

	rows, _ := repo.ListTransactions(ctx, wallet.OwnerRider, riderA, 1)
	if rows[0].Type != wallet.TxRefund || rows[0].TripID != tripOne {
		t.Fatalf("ledger %+v", rows[0])
	}
}

func TestRefundingAFeeStillOwedWaivesIt(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewWalletRepository(pool)
	store := NewOperationsStore(repo)

	settleFee(t, repo, tripTwo, 1500)

	if dues, _ := repo.ListDues(ctx, riderA); len(dues) != 1 {
		t.Fatalf("dues %+v", dues)
	}

	_, rider, err := store.Refund(ctx, operations.RefundRecord{
		TripID: tripTwo, Amount: dec(1500), DriverAmount: dec(0), Reason: "the driver never came", CreatedBy: staffS, IdempotencyKey: "fee",
	})
	if err != nil || rider.Balance.String() != "0" {
		t.Fatalf("refund %+v %v", rider, err)
	}

	if dues, _ := repo.ListDues(ctx, riderA); len(dues) != 0 {
		t.Fatalf("still owed %+v", dues)
	}
}

func TestRefundsRushedAtOnceStayWithinTheFare(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewWalletRepository(pool)
	store := NewOperationsStore(repo)

	fund(t, repo, riderA, 10000)
	settleWalletTrip(t, repo, tripThree, 5000)

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		ok, over int
	)

	for i := 0; i < 5; i++ {
		wg.Add(1)

		go func(i int) {
			defer wg.Done()

			_, _, err := store.Refund(ctx, operations.RefundRecord{
				TripID: tripThree, Amount: dec(2000), DriverAmount: dec(500), Reason: "rush", CreatedBy: staffS,
				IdempotencyKey: "rush-" + string(rune('a'+i)),
			})

			mu.Lock()
			defer mu.Unlock()

			switch {
			case err == nil:
				ok++
			case errors.Is(err, operations.ErrRefundTooLarge):
				over++
			default:
				t.Errorf("unexpected: %v", err)
			}
		}(i)
	}

	wg.Wait()

	if ok != 2 || over != 3 {
		t.Fatalf("ok %d, too large %d", ok, over)
	}
}

func TestAPayoutIsHeldThenPaidOrGivenBack(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewWalletRepository(pool)
	store := NewOperationsStore(repo)
	now := time.Now()

	if _, _, err := repo.ApplyMovement(ctx, wallet.MovementInput{
		OwnerType: wallet.OwnerDriver, OwnerID: driverD, Type: wallet.TxTopUp, Amount: dec(50000),
	}); err != nil {
		t.Fatal(err)
	}

	first, driver, hold, err := store.RequestPayout(ctx, operations.PayoutRecord{DriverID: driverD, Amount: dec(20000), Destination: "ZainCash +9647500000009", IdempotencyKey: "p1"})
	if err != nil || first.Status != operations.PayoutPending || driver.Balance.String() != "30000" || hold.Type != wallet.TxPayout || !hold.Amount.Equal(dec(-20000)) {
		t.Fatalf("request %+v %+v %+v %v", first, driver, hold, err)
	}

	if _, _, _, err := store.RequestPayout(ctx, operations.PayoutRecord{DriverID: driverD, Amount: dec(10000), IdempotencyKey: "p2"}); !errors.Is(err, operations.ErrPayoutOpen) {
		t.Fatalf("a second open request: %v", err)
	}

	if got, _ := driverBalance(t, repo, driverD); got != "30000" {
		t.Fatalf("a refused request held money: %s", got)
	}

	approved, err := store.SetPayoutStatus(ctx, first.ID, operations.PayoutApproved, staffS, "", now)
	if err != nil || approved.Status != operations.PayoutApproved || approved.ApprovedAt == nil {
		t.Fatalf("approve %+v %v", approved, err)
	}

	if _, err := store.SetPayoutStatus(ctx, first.ID, operations.PayoutApproved, staffS, "", now); !errors.Is(err, operations.ErrPayoutState) {
		t.Fatalf("approving twice: %v", err)
	}

	paid, err := store.SetPayoutStatus(ctx, first.ID, operations.PayoutPaid, staffS, "ZC-778899", now)
	if err != nil || paid.Status != operations.PayoutPaid || paid.PaidReference != "ZC-778899" {
		t.Fatalf("paid %+v %v", paid, err)
	}

	if _, err := store.RejectPayout(ctx, first.ID, staffS, "too late", now); !errors.Is(err, operations.ErrPayoutState) {
		t.Fatalf("rejecting a paid one: %v", err)
	}

	// Paid, it is closed: another may be asked for, and rejected.
	second, _, _, err := store.RequestPayout(ctx, operations.PayoutRecord{DriverID: driverD, Amount: dec(10000), IdempotencyKey: "p3"})
	if err != nil {
		t.Fatal(err)
	}

	rejected, err := store.RejectPayout(ctx, second.ID, staffS, "no such ZainCash number", now)
	if err != nil || rejected.Status != operations.PayoutRejected || rejected.RejectReason != "no such ZainCash number" {
		t.Fatalf("reject %+v %v", rejected, err)
	}

	if got, _ := driverBalance(t, repo, driverD); got != "30000" {
		t.Fatalf("the rejected amount did not come back: %s", got)
	}

	rows, _ := repo.ListTransactions(ctx, wallet.OwnerDriver, driverD, 1)
	if rows[0].Type != wallet.TxPayoutReturn || !rows[0].Amount.Equal(dec(10000)) {
		t.Fatalf("ledger %+v", rows[0])
	}

	if _, _, _, err := store.RequestPayout(ctx, operations.PayoutRecord{DriverID: driverD, Amount: dec(40000), IdempotencyKey: "p4"}); !errors.Is(err, wallet.ErrInsufficientFunds) {
		t.Fatalf("more than the balance: %v", err)
	}

	if _, _, _, err := store.RequestPayout(ctx, operations.PayoutRecord{DriverID: driverD, Amount: dec(10000), IdempotencyKey: "p1"}); !errors.Is(err, wallet.ErrDuplicateRequest) {
		t.Fatalf("the same key: %v", err)
	}

	if found, ok, err := store.FindPayoutByKey(ctx, driverD, "p1"); err != nil || !ok || found.ID != first.ID {
		t.Fatalf("by key %+v %v %v", found, ok, err)
	}

	queue, err := store.ListPayouts(ctx, "", "", 0, 10)
	if err != nil || len(queue) != 2 || queue[0].ID != first.ID {
		t.Fatalf("the queue, oldest first %+v %v", queue, err)
	}

	mine, err := store.ListPayouts(ctx, driverD, operations.PayoutRejected, 0, 10)
	if err != nil || len(mine) != 1 || mine[0].ID != second.ID {
		t.Fatalf("the driver's rejected %+v %v", mine, err)
	}

	if _, err := store.GetPayout(ctx, "00000000-0000-4000-8000-000000000000"); !errors.Is(err, operations.ErrPayoutNotFound) {
		t.Fatalf("an unknown payout: %v", err)
	}
}

func TestASuspendedDriverCannotAskForAPayout(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewWalletRepository(pool)
	store := NewOperationsStore(repo)

	if _, _, err := store.Adjust(ctx, operations.AdjustRecord{
		OwnerType: wallet.OwnerDriver, OwnerID: driverD, Amount: dec(-60000), Reason: "commission owed", CreatedBy: staffS, IdempotencyKey: "s1",
	}); err != nil {
		t.Fatal(err)
	}

	if _, _, _, err := store.RequestPayout(ctx, operations.PayoutRecord{DriverID: driverD, Amount: dec(10000), IdempotencyKey: "s2"}); !errors.Is(err, wallet.ErrWalletBlocked) {
		t.Fatalf("a suspended driver: %v", err)
	}
}

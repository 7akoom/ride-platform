package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/transfer"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

const (
	riderC  = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	driverD = "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	tripOne = "11111111-1111-4111-8111-111111111111"
	tripTwo = "22222222-2222-4222-8222-222222222222"
)

func dec(v int64) decimal.Decimal { return decimal.NewFromInt(v) }

func moneyRequest(code, key string, open bool, amount int64) transfer.MoneyRequest {
	r := transfer.MoneyRequest{
		Code: code, RequesterRiderID: riderA, RequesterPhone: "+9647500000001", Open: open,
		CurrencyCode: "IQD", Amount: dec(amount), Note: "lunch", IdempotencyKey: key,
		ExpiresAt: time.Now().Add(time.Hour),
	}

	if !open {
		r.PayerRiderID, r.PayerPhone = riderB, "+9647500000002"
	}

	return r
}

func payment(payer, key string, amount int64) transfer.Record {
	return record(payer, riderA, key, amount)
}

func TestAMoneyRequestIsStoredWithItsEvent(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	store := NewTransferStore(NewWalletRepository(pool))

	targeted, err := store.CreateRequest(ctx, moneyRequest("CODEAAAAA2", "k1", false, 3000))
	if err != nil || targeted.ID == "" || targeted.Status != transfer.RequestPending || targeted.PayerRiderID != riderB {
		t.Fatalf("created %+v %v", targeted, err)
	}

	open, err := store.CreateRequest(ctx, moneyRequest("CODEBBBBB2", "k2", true, 3000))
	if err != nil || !open.Open || open.PayerRiderID != "" {
		t.Fatalf("created %+v %v", open, err)
	}

	var events int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM outbox_events WHERE event_type = 'wallet.money_requested'`).Scan(&events); err != nil || events != 1 {
		t.Fatalf("only the targeted request tells someone: %d %v", events, err)
	}

	if _, err := store.CreateRequest(ctx, moneyRequest("CODEAAAAA2", "k3", true, 3000)); !errors.Is(err, transfer.ErrCodeTaken) {
		t.Fatalf("a taken code: %v", err)
	}

	if _, err := store.CreateRequest(ctx, moneyRequest("CODECCCCC2", "k1", true, 3000)); !errors.Is(err, wallet.ErrDuplicateRequest) {
		t.Fatalf("a used key: %v", err)
	}

	if found, ok, err := store.FindRequestByKey(ctx, riderA, "k2"); err != nil || !ok || found.ID != open.ID {
		t.Fatalf("by key %+v %v %v", found, ok, err)
	}

	incoming, err := store.ListRequests(ctx, riderB, "incoming", "", time.Now(), 0, 10)
	if err != nil || len(incoming) != 1 || incoming[0].ID != targeted.ID {
		t.Fatalf("incoming %+v %v", incoming, err)
	}

	outgoing, err := store.ListRequests(ctx, riderA, "outgoing", transfer.RequestPending, time.Now(), 0, 10)
	if err != nil || len(outgoing) != 2 {
		t.Fatalf("outgoing %+v %v", outgoing, err)
	}

	expired, err := store.ListRequests(ctx, riderA, "outgoing", transfer.RequestExpired, time.Now().Add(2*time.Hour), 0, 10)
	if err != nil || len(expired) != 2 {
		t.Fatalf("expired later %+v %v", expired, err)
	}

	declined, err := store.CloseRequest(ctx, targeted.ID, transfer.RequestDeclined, time.Now())
	if err != nil || declined.Status != transfer.RequestDeclined || declined.ClosedAt == nil {
		t.Fatalf("declined %+v %v", declined, err)
	}

	if _, err := store.CloseRequest(ctx, targeted.ID, transfer.RequestCancelled, time.Now()); !errors.Is(err, transfer.ErrRequestNotPending) {
		t.Fatalf("closing twice: %v", err)
	}

	if _, err := store.CloseRequest(ctx, open.ID, transfer.RequestCancelled, time.Now().Add(2*time.Hour)); !errors.Is(err, transfer.ErrRequestNotPending) {
		t.Fatalf("closing an expired one: %v", err)
	}
}

func TestAnOpenRequestIsPaidOnceWhoeverRushesToPayIt(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewWalletRepository(pool)
	store := NewTransferStore(repo)

	fund(t, repo, riderB, 10000)
	fund(t, repo, riderC, 10000)

	request, err := store.CreateRequest(ctx, moneyRequest("CODEOPEN22", "open", true, 3000))
	if err != nil {
		t.Fatal(err)
	}

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		paid []string
	)

	for i := 0; i < 8; i++ {
		wg.Add(1)

		go func(i int) {
			defer wg.Done()

			payer := riderB
			if i%2 == 1 {
				payer = riderC
			}

			done, _, _, err := store.PayRequest(ctx, request.ID, payment(payer, "request:"+request.ID, 3000), time.Now())

			mu.Lock()
			defer mu.Unlock()

			switch {
			case err == nil:
				paid = append(paid, done.PayerRiderID)
			case errors.Is(err, transfer.ErrRequestNotPending):
			default:
				t.Errorf("pay %d: %v", i, err)
			}
		}(i)
	}

	wg.Wait()

	if len(paid) != 1 {
		t.Fatalf("paid %d times", len(paid))
	}

	if got := balance(t, repo, riderA); got != "3000" {
		t.Fatalf("requester %s", got)
	}

	total := dec(0)
	for _, rider := range []string{riderB, riderC} {
		b, _ := decimal.NewFromString(balance(t, repo, rider))
		total = total.Add(b)
	}

	if !total.Equal(dec(17000)) {
		t.Fatalf("the payers hold %s", total)
	}

	found, ok, err := store.FindRequestByCode(ctx, "CODEOPEN22")
	if err != nil || !ok || found.Status != transfer.RequestPaid || found.PayerRiderID != paid[0] || found.TransferID == "" {
		t.Fatalf("found %+v %v %v", found, ok, err)
	}

	// The payer of an open request sees it among their incoming ones.
	incoming, err := store.ListRequests(ctx, paid[0], "incoming", transfer.RequestPaid, time.Now(), 0, 10)
	if err != nil || len(incoming) != 1 {
		t.Fatalf("incoming %+v %v", incoming, err)
	}
}

func TestPayingARequestWithoutTheMoneyMovesNothing(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewWalletRepository(pool)
	store := NewTransferStore(repo)

	fund(t, repo, riderB, 1000)

	request, err := store.CreateRequest(ctx, moneyRequest("CODEPOOR22", "poor", false, 3000))
	if err != nil {
		t.Fatal(err)
	}

	if _, _, _, err := store.PayRequest(ctx, request.ID, payment(riderB, "request:"+request.ID, 3000), time.Now()); !errors.Is(err, wallet.ErrInsufficientFunds) {
		t.Fatalf("got %v", err)
	}

	found, _, _ := store.FindRequestByCode(ctx, "CODEPOOR22")
	if found.Status != transfer.RequestPending || balance(t, repo, riderB) != "1000" {
		t.Fatalf("found %+v", found)
	}

	if _, _, _, err := store.PayRequest(ctx, request.ID, payment(riderB, "request:"+request.ID, 3000), time.Now().Add(2*time.Hour)); !errors.Is(err, transfer.ErrRequestNotPending) {
		t.Fatalf("an expired request: %v", err)
	}
}

func settleFee(t *testing.T, repo *WalletRepository, tripID string, fee int64) wallet.Settlement {
	t.Helper()

	settlement, err := repo.SettleTrip(context.Background(), wallet.SettleInput{
		TripID: tripID, RiderID: riderA, DriverID: driverD, CurrencyCode: "IQD",
		PaymentMethod: wallet.PaymentCash, FareAmount: dec(fee), CommissionRate: decimal.RequireFromString("0.1"),
		CommissionAmount: dec(fee / 10), DriverEarning: dec(fee - fee/10), SuspensionFloor: dec(-100000),
		Kind: wallet.SettlementCancellation,
	})
	if err != nil {
		t.Fatal(err)
	}

	return settlement
}

func TestMoneyReachingTheWalletPaysTheOldestFeesFirst(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewWalletRepository(pool)

	fund(t, repo, riderA, 500)

	// 500 of the first fee is paid from the wallet; 1000 stays owed.
	if s := settleFee(t, repo, tripOne, 1500); !s.DueAmount.Equal(dec(1000)) {
		t.Fatalf("first fee %+v", s)
	}

	time.Sleep(10 * time.Millisecond)

	if s := settleFee(t, repo, tripTwo, 2000); !s.DueAmount.Equal(dec(2000)) {
		t.Fatalf("second fee %+v", s)
	}

	start := time.Now()

	fund(t, repo, riderA, 1500)

	if got := balance(t, repo, riderA); got != "0" {
		t.Fatalf("balance %s", got)
	}

	dues, err := repo.ListDues(ctx, riderA)
	if err != nil || len(dues) != 1 || dues[0].TripID != tripTwo || !dues[0].Outstanding().Equal(dec(1500)) {
		t.Fatalf("dues %+v %v", dues, err)
	}

	first, _, err := repo.FindSettlement(ctx, tripOne)
	if err != nil || !first.DuePaid.Equal(dec(1000)) {
		t.Fatalf("first %+v %v", first, err)
	}

	fund(t, repo, riderA, 4000)

	if got := balance(t, repo, riderA); got != "2500" {
		t.Fatalf("balance %s", got)
	}

	if dues, _ := repo.ListDues(ctx, riderA); len(dues) != 0 {
		t.Fatalf("still owed %+v", dues)
	}

	statement, err := repo.Statement(ctx, wallet.StatementQuery{
		OwnerType: wallet.OwnerRider, OwnerID: riderA, From: start, To: time.Now().Add(time.Second), Limit: 50,
	})
	if err != nil {
		t.Fatal(err)
	}

	if !statement.Opening.Equal(dec(0)) || !statement.Closing.Equal(dec(2500)) ||
		!statement.TotalIn.Equal(dec(5500)) || !statement.TotalOut.Equal(dec(3000)) || len(statement.Entries) != 5 {
		t.Fatalf("statement %+v", statement)
	}

	// Newest first, each balance the one after the row below it.
	want := []wallet.TransactionType{wallet.TxDuePayment, wallet.TxTopUp, wallet.TxDuePayment, wallet.TxDuePayment, wallet.TxTopUp}
	for i, entry := range statement.Entries {
		if entry.Type != want[i] {
			t.Fatalf("entry %d is %s, want %s", i, entry.Type, want[i])
		}

		if i+1 < len(statement.Entries) {
			below := statement.Entries[i+1]
			if !below.BalanceAfter.Add(entry.Amount).Equal(entry.BalanceAfter) {
				t.Fatalf("entry %d: %s + %s != %s", i, below.BalanceAfter, entry.Amount, entry.BalanceAfter)
			}
		}
	}

	outs, err := repo.Statement(ctx, wallet.StatementQuery{
		OwnerType: wallet.OwnerRider, OwnerID: riderA, Direction: "out",
		Types: []wallet.TransactionType{wallet.TxDuePayment}, From: start, To: time.Now().Add(time.Second), Offset: 1, Limit: 1,
	})
	if err != nil || len(outs.Entries) != 1 || !outs.TotalOut.Equal(dec(3000)) || !outs.TotalIn.IsZero() {
		t.Fatalf("filtered %+v %v", outs, err)
	}

	earlier, err := repo.Statement(ctx, wallet.StatementQuery{
		OwnerType: wallet.OwnerRider, OwnerID: riderA, From: start.Add(-time.Hour), To: start, Limit: 50,
	})
	if err != nil || !earlier.Closing.IsZero() || !earlier.Opening.IsZero() || len(earlier.Entries) != 2 {
		t.Fatalf("before %+v %v", earlier, err)
	}

	nobody, err := repo.Statement(ctx, wallet.StatementQuery{
		OwnerType: wallet.OwnerRider, OwnerID: riderC, From: start, To: time.Now(), Limit: 50,
	})
	if err != nil || len(nobody.Entries) != 0 || nobody.CurrencyCode == "" {
		t.Fatalf("no wallet %+v %v", nobody, err)
	}

}

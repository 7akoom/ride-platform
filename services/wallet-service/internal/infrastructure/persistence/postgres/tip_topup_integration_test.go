package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/tips"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/topup"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

const riderOther = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"

func TestARiderTipsACompletedTripOnce(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewWalletRepository(pool)
	store := NewTipStore(repo)
	past := time.Now().Add(-time.Hour)

	tip := func(rider, trip, key string, amount int64) (tips.Tip, wallet.Wallet, error) {
		return store.Tip(ctx, tips.Record{TripID: trip, RiderID: rider, Amount: dec(amount), IdempotencyKey: key}, past)
	}

	if _, _, err := tip(riderA, tripOne, "t0", 1000); !errors.Is(err, tips.ErrTripNotFound) {
		t.Fatalf("an unsettled trip: %v", err)
	}

	fund(t, repo, riderA, 10000)
	settleWalletTrip(t, repo, tripOne, 5000)
	driverBefore, _ := driverBalance(t, repo, driverD)

	if _, _, err := tip(riderOther, tripOne, "t1", 1000); !errors.Is(err, tips.ErrTripNotFound) {
		t.Fatalf("another rider's trip: %v", err)
	}

	if _, _, err := tip(riderA, tripOne, "t2", 6000); !errors.Is(err, wallet.ErrInsufficientFunds) {
		t.Fatalf("more than the rider holds: %v", err)
	}

	given, rider, err := tip(riderA, tripOne, "t3", 1500)
	if err != nil || rider.Balance.String() != "3500" || given.DriverID != driverD || given.CurrencyCode != "IQD" {
		t.Fatalf("tip %+v %+v %v", given, rider, err)
	}

	after, _ := driverBalance(t, repo, driverD)
	if dec(0).Add(decimalOf(t, after)).Sub(decimalOf(t, driverBefore)).String() != "1500" {
		t.Fatalf("the driver got %s -> %s", driverBefore, after)
	}

	if _, _, err := tip(riderA, tripOne, "t4", 500); !errors.Is(err, tips.ErrAlreadyTipped) {
		t.Fatalf("a second tip: %v", err)
	}

	settlement, _, err := repo.FindSettlement(ctx, tripOne)
	if err != nil || settlement.TipAmount.String() != "1500" {
		t.Fatalf("settlement %+v %v", settlement, err)
	}

	var events int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE event_type = 'wallet.tip_received' AND aggregate_id = $1`, given.ID).Scan(&events); err != nil || events != 1 {
		t.Fatalf("events %d %v", events, err)
	}

	rows, _ := repo.ListTransactions(ctx, wallet.OwnerDriver, driverD, 1)
	if rows[0].Type != wallet.TxTip || rows[0].TripID != tripOne {
		t.Fatalf("driver ledger %+v", rows[0])
	}

	if found, ok, err := store.FindByKey(ctx, riderA, "t3"); err != nil || !ok || found.ID != given.ID {
		t.Fatalf("by key %+v %v %v", found, ok, err)
	}
}

func decimalOf(t *testing.T, s string) wallet.Money {
	t.Helper()

	return decimal.RequireFromString(s)
}

func TestOnlyARecentCompletedTripIsTippable(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewWalletRepository(pool)
	store := NewTipStore(repo)

	fund(t, repo, riderA, 10000)
	settleWalletTrip(t, repo, tripOne, 1000)
	settleFee(t, repo, tripTwo, 1000)

	if _, _, err := store.Tip(ctx, tips.Record{TripID: tripTwo, RiderID: riderA, Amount: dec(500), IdempotencyKey: "fee"}, time.Now().Add(-time.Hour)); !errors.Is(err, tips.ErrNotTippable) {
		t.Fatalf("a fee: %v", err)
	}

	if _, _, err := store.Tip(ctx, tips.Record{TripID: tripOne, RiderID: riderA, Amount: dec(500), IdempotencyKey: "late"}, time.Now().Add(time.Hour)); !errors.Is(err, tips.ErrNotTippable) {
		t.Fatalf("too late: %v", err)
	}
}

func TestTipsRushedAtOnceGiveOnce(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewWalletRepository(pool)
	store := NewTipStore(repo)

	fund(t, repo, riderA, 10000)
	settleWalletTrip(t, repo, tripThree, 1000)

	var (
		wg         sync.WaitGroup
		mu         sync.Mutex
		ok, tipped int
	)

	for i := 0; i < 6; i++ {
		wg.Add(1)

		go func(i int) {
			defer wg.Done()

			_, _, err := store.Tip(ctx, tips.Record{TripID: tripThree, RiderID: riderA, Amount: dec(500), IdempotencyKey: "rush-" + string(rune('a'+i))}, time.Now().Add(-time.Hour))

			mu.Lock()
			defer mu.Unlock()

			switch {
			case err == nil:
				ok++
			case errors.Is(err, tips.ErrAlreadyTipped):
				tipped++
			default:
				t.Errorf("unexpected: %v", err)
			}
		}(i)
	}

	wg.Wait()

	if ok != 1 || tipped != 5 {
		t.Fatalf("ok %d, already tipped %d", ok, tipped)
	}
}

func TestTopUpsAreKeptPerProviderAndOwner(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewTopUpRepository(pool)

	created, err := repo.Create(ctx, topup.TopUp{
		OwnerType: wallet.OwnerRider, OwnerID: riderA, Provider: "zaincash", Amount: dec(5000), CurrencyCode: "IQD", Status: topup.StatusPending,
	})
	if err != nil || created.ExternalReferenceID == "" || created.OwnerType != wallet.OwnerRider || created.Provider != "zaincash" {
		t.Fatalf("created %+v %v", created, err)
	}

	if _, err := repo.FindForOwner(ctx, wallet.OwnerRider, riderOther, created.ID); !errors.Is(err, topup.ErrTopUpNotFound) {
		t.Fatalf("another rider's: %v", err)
	}

	if _, err := repo.FindForOwner(ctx, wallet.OwnerDriver, riderA, created.ID); !errors.Is(err, topup.ErrTopUpNotFound) {
		t.Fatalf("the same id as a driver: %v", err)
	}

	withID, err := repo.SetProviderTransactionID(ctx, created.ExternalReferenceID, "zc-123")
	if err != nil || withID.ProviderTransactionID != "zc-123" {
		t.Fatalf("provider id %+v %v", withID, err)
	}

	done, err := repo.MarkSucceeded(ctx, created.ExternalReferenceID)
	if err != nil || done.Status != topup.StatusSucceeded {
		t.Fatalf("succeeded %+v %v", done, err)
	}

	// A late failure never undoes a success.
	if _, err := repo.MarkFailed(ctx, created.ExternalReferenceID, "late"); !errors.Is(err, topup.ErrTopUpNotFound) {
		t.Fatalf("a late failure: %v", err)
	}

	mine, err := repo.FindForOwner(ctx, wallet.OwnerRider, riderA, created.ID)
	if err != nil || mine.Status != topup.StatusSucceeded {
		t.Fatalf("mine %+v %v", mine, err)
	}

	if _, err := repo.FindByExternalReferenceID(ctx, "not-a-reference"); !errors.Is(err, topup.ErrTopUpNotFound) {
		t.Fatalf("an unknown reference: %v", err)
	}
}

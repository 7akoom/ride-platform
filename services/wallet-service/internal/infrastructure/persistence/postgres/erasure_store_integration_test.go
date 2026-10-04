package postgres

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/erasure"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

func TestErasureStoreClosesTheWallets(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewWalletRepository(pool)
	driverID := "dddddddd-dddd-4ddd-8ddd-dddddddddddd"

	fund(t, repo, riderA, 10000)

	if _, _, err := NewTransferStore(repo).Send(ctx, record(riderA, riderB, "k1", 4000)); err != nil {
		t.Fatal(err)
	}

	if _, _, err := repo.ApplyMovement(ctx, wallet.MovementInput{
		OwnerType: wallet.OwnerDriver, OwnerID: driverID, Type: wallet.TxTopUp, Amount: decimal.NewFromInt(1500),
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Exec(ctx,
		`INSERT INTO money_requests (code, requester_rider_id, requester_phone, payer_rider_id, payer_phone,
		     currency_code, amount, idempotency_key, expires_at)
		 VALUES ('ABC123', $2, '+9647500000002', $1, '+9647500000001', 'IQD', 500, 'm1', now() + interval '1 day')`,
		riderA, riderB); err != nil {
		t.Fatal(err)
	}

	store := NewErasureStore(repo)
	account := erasure.Account{IdentityID: "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", RiderID: riderA, DriverID: driverID}

	for i := 0; i < 2; i++ {
		if err := store.Erase(ctx, account); err != nil {
			t.Fatal(err)
		}
	}

	for owner, id := range map[wallet.OwnerType]string{wallet.OwnerRider: riderA, wallet.OwnerDriver: driverID} {
		w, err := repo.FindWallet(ctx, owner, id)
		if err != nil || !w.Balance.IsZero() || !w.Blocked {
			t.Fatalf("%s wallet %+v %v", owner, w, err)
		}
	}

	count := func(query string, args ...any) int {
		var n int
		if err := pool.QueryRow(ctx, query, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}

		return n
	}

	if count(`SELECT count(*) FROM wallet_transactions WHERE description = $1`, ForfeitDescription) != 2 {
		t.Fatal("want one forfeit row per wallet, and none on the second run")
	}

	if count(`SELECT count(*) FROM wallet_transfers WHERE sender_rider_id = $1 AND sender_phone <> ''`, riderA) != 0 {
		t.Fatal("the sender's phone is still on the transfer")
	}

	if count(`SELECT count(*) FROM wallet_transfers WHERE recipient_rider_id = $1 AND recipient_phone <> ''`, riderB) != 1 {
		t.Fatal("the other rider's phone was erased")
	}

	if count(`SELECT count(*) FROM money_requests WHERE status = 'cancelled' AND (payer_phone = '' OR payer_phone IS NULL) AND requester_phone <> ''`) != 1 {
		t.Fatal("the money request was not cancelled, or the wrong phone was erased")
	}

	if w, _ := repo.FindWallet(ctx, wallet.OwnerRider, riderB); w.Balance.String() != "4000" || w.Blocked {
		t.Fatalf("the other rider's wallet changed: %+v", w)
	}
}

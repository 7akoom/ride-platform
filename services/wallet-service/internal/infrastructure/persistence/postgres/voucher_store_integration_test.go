package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/voucher"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

const staffS = "5555aaaa-5555-4555-8555-555555555555"

type issued struct {
	batch voucher.Batch
	codes []string
}

// issue makes a batch of n codes the way the service does, and returns the
// codes so the test can redeem them.
func issue(t *testing.T, store *VoucherStore, codec *voucher.Codec, key string, n int, amount int64, expires time.Time) issued {
	t.Helper()

	codes := make([]string, 0, n)
	sealed := make([]voucher.Sealed, 0, n)

	for len(codes) < n {
		code, err := codec.NewCode()
		if err != nil {
			t.Fatal(err)
		}

		hash := codec.Hash(code)

		box, err := codec.Seal(code, hash)
		if err != nil {
			t.Fatal(err)
		}

		codes = append(codes, code)
		sealed = append(sealed, voucher.Sealed{Hash: hash, Sealed: box})
	}

	batch, err := store.CreateBatch(context.Background(), voucher.Batch{
		Label: "E2E " + key, Seller: "zaincash", Amount: dec(amount), CurrencyCode: "IQD",
		ExpiresAt: expires, CreatedBy: staffS, IdempotencyKey: key,
	}, sealed)
	if err != nil {
		t.Fatal(err)
	}

	return issued{batch: batch, codes: codes}
}

func testCodec(t *testing.T) *voucher.Codec {
	t.Helper()

	codec, err := voucher.NewCodec("integration-test-voucher-key")
	if err != nil {
		t.Fatal(err)
	}

	return codec
}

func TestABatchIsExportedOnceAndOnlyThenRedeemable(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewWalletRepository(pool)
	store := NewVoucherStore(repo)
	codec := testCodec(t)
	now := time.Now()

	b := issue(t, store, codec, "b1", 3, 5000, now.Add(24*time.Hour))

	if b.batch.Number < 1 || b.batch.Status != voucher.BatchCreated || b.batch.Quantity != 3 || b.batch.CurrencyCode != "IQD" {
		t.Fatalf("batch %+v", b.batch)
	}

	if _, err := store.CreateBatch(ctx, voucher.Batch{
		Label: "again", Amount: dec(1), CurrencyCode: "IQD", ExpiresAt: now.Add(time.Hour), CreatedBy: staffS, IdempotencyKey: "b1",
	}, []voucher.Sealed{{Hash: codec.Hash("ZZZZZZZZZZZZZZZZ"), Sealed: []byte{1}}}); !errors.Is(err, wallet.ErrDuplicateRequest) {
		t.Fatalf("the same key: %v", err)
	}

	if found, ok, err := store.FindBatchByKey(ctx, staffS, "b1"); err != nil || !ok || found.ID != b.batch.ID {
		t.Fatalf("by key %+v %v %v", found, ok, err)
	}

	// Not exported: as if the code did not exist.
	if _, err := store.Redeem(ctx, riderA, codec.Hash(b.codes[0]), now); !errors.Is(err, voucher.ErrCodeNotValid) {
		t.Fatalf("before the export: %v", err)
	}

	// An export that cannot open a code changes nothing.
	if _, _, err := store.ExportBatch(ctx, b.batch.ID, staffS, now, func([]byte, []byte) (string, error) {
		return "", errors.New("wrong key")
	}); err == nil {
		t.Fatal("a failed open was exported")
	}

	batch, exported, err := store.ExportBatch(ctx, b.batch.ID, staffS, now, codec.Open)
	if err != nil || batch.Status != voucher.BatchExported || batch.ExportedAt == nil || len(exported) != 3 {
		t.Fatalf("export %+v %+v %v", batch, exported, err)
	}

	for i, e := range exported {
		if e.Serial != voucher.Serial(batch.Number, i+1) || e.Code != b.codes[i] {
			t.Fatalf("exported %d: %+v, want %s", i, e, b.codes[i])
		}
	}

	var sealedLeft int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM vouchers WHERE batch_id = $1 AND code_sealed IS NOT NULL`, batch.ID).Scan(&sealedLeft); err != nil || sealedLeft != 0 {
		t.Fatalf("sealed codes left %d %v", sealedLeft, err)
	}

	if _, _, err := store.ExportBatch(ctx, b.batch.ID, staffS, now, codec.Open); !errors.Is(err, voucher.ErrNotExportable) {
		t.Fatalf("a second export: %v", err)
	}

	redeemed, err := store.Redeem(ctx, riderA, codec.Hash(b.codes[0]), now)
	if err != nil || redeemed.Wallet.Balance.String() != "5000" || redeemed.Transaction.Type != wallet.TxVoucher ||
		redeemed.Serial != exported[0].Serial || redeemed.Transaction.Description != "Voucher "+exported[0].Serial {
		t.Fatalf("redeemed %+v %v", redeemed, err)
	}

	// The same rider again: the same redemption, nothing more credited.
	again, err := store.Redeem(ctx, riderA, codec.Hash(b.codes[0]), now)
	if err != nil || again.Transaction.ID != redeemed.Transaction.ID || balance(t, repo, riderA) != "5000" {
		t.Fatalf("again %+v %v", again, err)
	}

	if _, err := store.Redeem(ctx, riderB, codec.Hash(b.codes[0]), now); !errors.Is(err, voucher.ErrCodeUsed) {
		t.Fatalf("another rider: %v", err)
	}

	got, err := store.GetVoucher(ctx, exported[0].Serial)
	if err != nil || got.Status != voucher.Redeemed || got.RedeemedByRiderID != riderA || got.TransactionID != redeemed.Transaction.ID {
		t.Fatalf("voucher %+v %v", got, err)
	}

	after, err := store.GetBatch(ctx, batch.ID)
	if err != nil || after.RedeemedCount != 1 || !after.RedeemedAmount.Equal(dec(5000)) {
		t.Fatalf("counts %+v %v", after, err)
	}

	if _, err := store.Redeem(ctx, riderA, codec.Hash("ABCDEFGHJKMNPQRS"), now); !errors.Is(err, voucher.ErrCodeNotValid) {
		t.Fatalf("an unknown code: %v", err)
	}
}

func TestVoidCancelAndExpiryStopAVoucher(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	store := NewVoucherStore(NewWalletRepository(pool))
	codec := testCodec(t)
	now := time.Now()

	b := issue(t, store, codec, "b2", 3, 1000, now.Add(time.Hour))
	if _, _, err := store.ExportBatch(ctx, b.batch.ID, staffS, now, codec.Open); err != nil {
		t.Fatal(err)
	}

	serial := voucher.Serial(b.batch.Number, 1)

	voided, err := store.VoidVoucher(ctx, serial, staffS, "card reported lost", now)
	if err != nil || voided.Status != voucher.Void || voided.VoidReason != "card reported lost" || voided.RedeemableAt(now) {
		t.Fatalf("voided %+v %v", voided, err)
	}

	if _, err := store.VoidVoucher(ctx, serial, staffS, "again", now); !errors.Is(err, voucher.ErrNotVoidable) {
		t.Fatalf("voiding twice: %v", err)
	}

	if _, err := store.Redeem(ctx, riderA, codec.Hash(b.codes[0]), now); !errors.Is(err, voucher.ErrCodeCancelled) {
		t.Fatalf("a void voucher: %v", err)
	}

	// Past its end.
	if _, err := store.Redeem(ctx, riderA, codec.Hash(b.codes[1]), now.Add(2*time.Hour)); !errors.Is(err, voucher.ErrCodeExpired) {
		t.Fatalf("an expired voucher: %v", err)
	}

	cancelled, err := store.CancelBatch(ctx, b.batch.ID, staffS, "sold by mistake", now)
	if err != nil || cancelled.Status != voucher.BatchCancelled || cancelled.VoidCount != 1 || cancelled.CancelReason != "sold by mistake" {
		t.Fatalf("cancelled %+v %v", cancelled, err)
	}

	if _, err := store.Redeem(ctx, riderA, codec.Hash(b.codes[2]), now); !errors.Is(err, voucher.ErrCodeCancelled) {
		t.Fatalf("a cancelled batch: %v", err)
	}

	if _, err := store.CancelBatch(ctx, b.batch.ID, staffS, "twice", now); !errors.Is(err, voucher.ErrNotCancellable) {
		t.Fatalf("cancelling twice: %v", err)
	}

	// A batch cancelled before its export loses its sealed codes too.
	c := issue(t, store, codec, "b3", 2, 1000, now.Add(time.Hour))
	if _, err := store.CancelBatch(ctx, c.batch.ID, staffS, "never sold", now); err != nil {
		t.Fatal(err)
	}

	var sealedLeft int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM vouchers WHERE batch_id = $1 AND code_sealed IS NOT NULL`, c.batch.ID).Scan(&sealedLeft); err != nil || sealedLeft != 0 {
		t.Fatalf("sealed codes left %d %v", sealedLeft, err)
	}

	if _, _, err := store.ExportBatch(ctx, c.batch.ID, staffS, now, codec.Open); !errors.Is(err, voucher.ErrNotExportable) {
		t.Fatalf("exporting a cancelled batch: %v", err)
	}

	listed, err := store.ListBatches(ctx, voucher.BatchCancelled, 0, 10)
	if err != nil || len(listed) != 2 || listed[0].ID != c.batch.ID {
		t.Fatalf("listed %+v %v", listed, err)
	}

	if _, err := store.GetBatch(ctx, "not-a-uuid"); !errors.Is(err, voucher.ErrBatchNotFound) {
		t.Fatalf("a bad id: %v", err)
	}
}

func TestAVoucherIsRedeemedOnceWhoeverRushesToIt(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewWalletRepository(pool)
	store := NewVoucherStore(repo)
	codec := testCodec(t)
	now := time.Now()

	b := issue(t, store, codec, "rush", 1, 3000, now.Add(time.Hour))
	if _, _, err := store.ExportBatch(ctx, b.batch.ID, staffS, now, codec.Open); err != nil {
		t.Fatal(err)
	}

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		ok   int
		used int
	)

	for i := 0; i < 8; i++ {
		rider := riderA
		if i%2 == 1 {
			rider = riderB
		}

		wg.Add(1)

		go func() {
			defer wg.Done()

			_, err := store.Redeem(ctx, rider, codec.Hash(b.codes[0]), now)

			mu.Lock()
			defer mu.Unlock()

			switch {
			case err == nil:
				ok++
			case errors.Is(err, voucher.ErrCodeUsed):
				used++
			default:
				t.Errorf("unexpected: %v", err)
			}
		}()
	}

	wg.Wait()

	// The winner's own retries succeed too (the same redemption); the other
	// rider is told it is used. Only one credit exists.
	if ok+used != 8 || used == 0 || ok == 0 {
		t.Fatalf("ok %d used %d", ok, used)
	}

	var credits int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM wallet_transactions WHERE type = 'voucher'`).Scan(&credits); err != nil || credits != 1 {
		t.Fatalf("credits %d %v", credits, err)
	}
}

func TestAVoucherPaysTheRidersFeesFirst(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewWalletRepository(pool)
	store := NewVoucherStore(repo)
	codec := testCodec(t)
	now := time.Now()

	settleFee(t, repo, tripOne, 1500)

	b := issue(t, store, codec, "dues", 1, 5000, now.Add(time.Hour))
	if _, _, err := store.ExportBatch(ctx, b.batch.ID, staffS, now, codec.Open); err != nil {
		t.Fatal(err)
	}

	redeemed, err := store.Redeem(ctx, riderA, codec.Hash(b.codes[0]), now)
	if err != nil || redeemed.Wallet.Balance.String() != "3500" || redeemed.Transaction.BalanceAfter.String() != "5000" {
		t.Fatalf("redeemed %+v %v", redeemed, err)
	}

	if dues, _ := repo.ListDues(ctx, riderA); len(dues) != 0 {
		t.Fatalf("still owed %+v", dues)
	}
}

func TestWrongCodesAreCountedInTheirWindow(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	store := NewVoucherStore(NewWalletRepository(pool))
	now := time.Now()

	for i := 0; i < 3; i++ {
		if err := store.RecordFailure(ctx, riderA, now.Add(time.Duration(i)*time.Minute), now.Add(-time.Hour)); err != nil {
			t.Fatal(err)
		}
	}

	count, oldest, err := store.Failures(ctx, riderA, now.Add(-time.Second))
	if err != nil || count != 3 || !oldest.Equal(now.Truncate(time.Microsecond)) {
		t.Fatalf("failures %d %v %v", count, oldest, err)
	}

	if count, _, _ := store.Failures(ctx, riderB, now.Add(-time.Hour)); count != 0 {
		t.Fatalf("another rider %d", count)
	}

	// Recording forgets the rider's failures outside the window.
	if err := store.RecordFailure(ctx, riderA, now.Add(2*time.Hour), now.Add(90*time.Minute)); err != nil {
		t.Fatal(err)
	}

	var rows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM voucher_redeem_failures WHERE rider_id = $1`, riderA).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("rows %d %v", rows, err)
	}
}

func TestACodeIssuedTwiceIsRefusedAndNothingIsKept(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	store := NewVoucherStore(NewWalletRepository(pool))
	codec := testCodec(t)

	first := issue(t, store, codec, "first", 1, 1000, time.Now().Add(time.Hour))
	hash := codec.Hash(first.codes[0])

	_, err := store.CreateBatch(ctx, voucher.Batch{
		Label: "clash", Amount: dec(1000), CurrencyCode: "IQD", ExpiresAt: time.Now().Add(time.Hour), CreatedBy: staffS, IdempotencyKey: "clash",
	}, []voucher.Sealed{{Hash: hash, Sealed: []byte{1}}})
	if !errors.Is(err, voucher.ErrCodeTaken) {
		t.Fatalf("a taken code: %v", err)
	}

	if _, found, err := store.FindBatchByKey(ctx, staffS, "clash"); err != nil || found {
		t.Fatalf("the refused batch was kept: %v %v", found, err)
	}
}

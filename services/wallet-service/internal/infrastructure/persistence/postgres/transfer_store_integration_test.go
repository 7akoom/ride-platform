package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/transfer"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

// These tests run against a real, EMPTY, throw-away database:
//
//	WALLET_TEST_DATABASE_URL=postgres://... go test ./internal/infrastructure/persistence/postgres/
//
// They apply every migration's Up section themselves (psql must be on PATH)
// and drop everything afterwards. Without the variable they are skipped.

const (
	riderA = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	riderB = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	url := os.Getenv("WALLET_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("WALLET_TEST_DATABASE_URL is not set")
	}

	files, err := filepath.Glob("../../../../migrations/*.sql")
	if err != nil || len(files) == 0 {
		t.Fatalf("migrations: %v", err)
	}

	sort.Strings(files)

	psql := func(sql string) {
		cmd := exec.Command("psql", url, "-v", "ON_ERROR_STOP=1", "-q")
		cmd.Stdin = strings.NewReader(sql)

		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("psql: %v\n%s", err, out)
		}
	}

	dropAll := func() {
		psql(`DROP SCHEMA public CASCADE; CREATE SCHEMA public;`)
	}

	dropAll()

	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}

		up, _, _ := strings.Cut(string(raw), "-- +goose Down")
		psql(up)
	}

	t.Cleanup(dropAll)

	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(pool.Close)

	return pool
}

func fund(t *testing.T, repo *WalletRepository, riderID string, amount int64) {
	t.Helper()

	if _, _, err := repo.ApplyMovement(context.Background(), wallet.MovementInput{
		OwnerType: wallet.OwnerRider, OwnerID: riderID, Type: wallet.TxTopUp, Amount: decimal.NewFromInt(amount),
	}); err != nil {
		t.Fatal(err)
	}
}

func record(sender, recipient, key string, amount int64) transfer.Record {
	return transfer.Record{
		Transfer: transfer.Transfer{
			SenderRiderID: sender, RecipientRiderID: recipient, SenderPhone: "+9647500000001",
			RecipientPhone: "+9647500000002", CurrencyCode: "IQD", Amount: decimal.NewFromInt(amount),
			Note: "e2e", IdempotencyKey: key,
		},
		DailyAmount: decimal.NewFromInt(50000),
		DailyCount:  5,
	}
}

func balance(t *testing.T, repo *WalletRepository, riderID string) string {
	t.Helper()

	w, err := repo.FindWallet(context.Background(), wallet.OwnerRider, riderID)
	if err != nil {
		t.Fatal(err)
	}

	return w.Balance.String()
}

func TestATransferMovesMoneyWithItsLedgerAndEvent(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewWalletRepository(pool)
	store := NewTransferStore(repo)

	config, err := store.Config(ctx)
	if err != nil || !config.TransferMinAmount.Equal(decimal.NewFromInt(250)) || config.TransferDailyCount != 20 {
		t.Fatalf("config %+v %v", config, err)
	}

	fund(t, repo, riderA, 10000)

	sent, sender, err := store.Send(ctx, record(riderA, riderB, "k1", 4000))
	if err != nil || sender.Balance.String() != "6000" || sent.ID == "" {
		t.Fatalf("sent %+v, sender %+v, %v", sent, sender, err)
	}

	if got := balance(t, repo, riderB); got != "4000" {
		t.Fatalf("recipient %s", got)
	}

	rows, err := repo.ListTransactions(ctx, wallet.OwnerRider, riderB, 10)
	if err != nil || len(rows) != 1 || rows[0].Type != wallet.TxTransferIn || rows[0].TransferID != sent.ID {
		t.Fatalf("recipient ledger %+v %v", rows, err)
	}

	var events int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM outbox_events WHERE event_type = 'wallet.transfer_completed' AND aggregate_id = $1`, sent.ID).Scan(&events); err != nil || events != 1 {
		t.Fatalf("events %d %v", events, err)
	}

	if found, ok, err := store.FindByKey(ctx, riderA, "k1"); err != nil || !ok || found.ID != sent.ID {
		t.Fatalf("by key %+v %v %v", found, ok, err)
	}

	if _, _, err := store.Send(ctx, record(riderA, riderB, "k1", 4000)); !errors.Is(err, wallet.ErrDuplicateRequest) {
		t.Fatalf("the same key: %v", err)
	}

	if _, _, err := store.Send(ctx, record(riderA, riderB, "k2", 7000)); !errors.Is(err, wallet.ErrInsufficientFunds) {
		t.Fatalf("more than the balance: %v", err)
	}

	if got := balance(t, repo, riderA); got != "6000" {
		t.Fatalf("a refused send moved money: %s", got)
	}

	listed, err := store.List(ctx, riderB, 0, 10)
	if err != nil || len(listed) != 1 {
		t.Fatalf("listed %+v %v", listed, err)
	}
}

func TestTheDailyLimitsHoldUnderParallelSends(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewWalletRepository(pool)
	store := NewTransferStore(repo)

	fund(t, repo, riderA, 100000)

	const sends = 12

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		ok      int
		limited int
	)

	for i := 0; i < sends; i++ {
		wg.Add(1)

		go func(i int) {
			defer wg.Done()

			// Both riders send to each other at once too: the fixed lock
			// order keeps that from deadlocking.
			if i%4 == 0 {
				_, _, _ = store.Send(ctx, record(riderB, riderA, fmt.Sprintf("back-%d", i), 250))
			}

			_, _, err := store.Send(ctx, record(riderA, riderB, fmt.Sprintf("k-%d", i), 1000))

			mu.Lock()
			defer mu.Unlock()

			switch {
			case err == nil:
				ok++
			case errors.Is(err, transfer.ErrDailyCount):
				limited++
			default:
				t.Errorf("send %d: %v", i, err)
			}
		}(i)
	}

	wg.Wait()

	if ok != 5 || limited != sends-5 {
		t.Fatalf("sent %d, limited %d", ok, limited)
	}

	if _, _, err := store.Send(ctx, record(riderA, riderB, "big", 49000)); !errors.Is(err, transfer.ErrDailyCount) {
		t.Fatalf("after the count: %v", err)
	}
}

func TestTheDailyAmountIsSummed(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewWalletRepository(pool)
	store := NewTransferStore(repo)

	fund(t, repo, riderA, 100000)

	if _, _, err := store.Send(ctx, record(riderA, riderB, "a", 30000)); err != nil {
		t.Fatal(err)
	}

	if _, _, err := store.Send(ctx, record(riderA, riderB, "b", 30000)); !errors.Is(err, transfer.ErrDailyLimit) {
		t.Fatalf("past the day's amount: %v", err)
	}

	if _, _, err := store.Send(ctx, record(riderA, riderB, "c", 20000)); err != nil {
		t.Fatalf("up to it: %v", err)
	}
}

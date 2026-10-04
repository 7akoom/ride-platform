package postgres

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/operations"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/topup"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

// outboxPayloads returns the payloads of the events of a type, oldest first.
func outboxPayloads(t *testing.T, pool *pgxpool.Pool, eventType string) []map[string]any {
	t.Helper()

	rows, err := pool.Query(context.Background(),
		`SELECT payload FROM outbox_events WHERE event_type = $1 ORDER BY occurred_at, id`, eventType)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var out []map[string]any

	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}

		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatal(err)
		}

		out = append(out, payload)
	}

	return out
}

func TestADriverIsToldWhenSuspendedAndReinstatedOnlyOnTheCrossing(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	store := NewOperationsStore(NewWalletRepository(pool))

	adjust := func(key string, amount int64) {
		t.Helper()

		if _, _, err := store.Adjust(ctx, operations.AdjustRecord{
			OwnerType: wallet.OwnerDriver, OwnerID: driverD, Amount: dec(amount), Reason: "test", CreatedBy: staffS, IdempotencyKey: key,
		}); err != nil {
			t.Fatal(err)
		}
	}

	adjust("e1", -40000) // above the floor (-50000): nothing
	adjust("e2", -20000) // -60000: suspended
	adjust("e3", -5000)  // still suspended: nothing new
	adjust("e4", 30000)  // -35000: reinstated
	adjust("e5", 1000)   // still fine: nothing

	suspended := outboxPayloads(t, pool, EventDriverSuspended)
	reinstated := outboxPayloads(t, pool, EventDriverReinstated)

	if len(suspended) != 1 || suspended[0]["driver_id"] != driverD || suspended[0]["balance"] != "-60000" ||
		suspended[0]["amount_due"] != "60000" || suspended[0]["currency_code"] != "IQD" {
		t.Fatalf("suspended %v", suspended)
	}

	if len(reinstated) != 1 || reinstated[0]["balance"] != "-35000" {
		t.Fatalf("reinstated %v", reinstated)
	}

	// A rider's wallet never says either.
	fund(t, NewWalletRepository(pool), riderA, 1000)

	if n := len(outboxPayloads(t, pool, EventDriverSuspended)) + len(outboxPayloads(t, pool, EventDriverReinstated)); n != 2 {
		t.Fatalf("%d standing events", n)
	}
}

func TestPayoutsRefundsAndTopUpsAreAnnounced(t *testing.T) {
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

	first, _, _, err := store.RequestPayout(ctx, operations.PayoutRecord{DriverID: driverD, Amount: dec(20000), Destination: "ZainCash", IdempotencyKey: "p1"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.SetPayoutStatus(ctx, first.ID, operations.PayoutApproved, staffS, "", now); err != nil {
		t.Fatal(err)
	}

	if len(outboxPayloads(t, pool, EventPayoutPaid)) != 0 {
		t.Fatal("an approved payout was announced as paid")
	}

	if _, err := store.SetPayoutStatus(ctx, first.ID, operations.PayoutPaid, staffS, "ZC-1", now); err != nil {
		t.Fatal(err)
	}

	second, _, _, err := store.RequestPayout(ctx, operations.PayoutRecord{DriverID: driverD, Amount: dec(10000), IdempotencyKey: "p2"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.RejectPayout(ctx, second.ID, staffS, "wrong number", now); err != nil {
		t.Fatal(err)
	}

	paid := outboxPayloads(t, pool, EventPayoutPaid)
	rejected := outboxPayloads(t, pool, EventPayoutRejected)

	if len(paid) != 1 || paid[0]["payout_id"] != first.ID || paid[0]["driver_id"] != driverD || paid[0]["amount"] != "20000" {
		t.Fatalf("paid %v", paid)
	}

	if len(rejected) != 1 || rejected[0]["payout_id"] != second.ID || rejected[0]["reason"] != "wrong number" {
		t.Fatalf("rejected %v", rejected)
	}

	fund(t, repo, riderA, 10000)
	settleWalletTrip(t, repo, tripOne, 5000)

	made, _, err := store.Refund(ctx, operations.RefundRecord{
		TripID: tripOne, Amount: dec(2000), Reason: "long route", CreatedBy: staffS, IdempotencyKey: "r1",
	})
	if err != nil {
		t.Fatal(err)
	}

	refunds := outboxPayloads(t, pool, EventRefundIssued)
	if len(refunds) != 1 || refunds[0]["adjustment_id"] != made.ID || refunds[0]["rider_id"] != riderA ||
		refunds[0]["trip_id"] != tripOne || refunds[0]["amount"] != "2000" {
		t.Fatalf("refunds %v", refunds)
	}

	topups := NewTopUpRepository(pool)

	created, err := topups.Create(ctx, topup.TopUp{
		OwnerType: wallet.OwnerRider, OwnerID: riderA, Provider: "zaincash", Amount: dec(5000), CurrencyCode: "IQD", Status: topup.StatusPending,
	})
	if err != nil {
		t.Fatal(err)
	}

	fund(t, repo, riderA, 5000) // the credit comes before it is marked

	for range 2 { // a provider repeating its notification is announced once
		if _, err := topups.MarkSucceeded(ctx, created.ExternalReferenceID); err != nil {
			t.Fatal(err)
		}
	}

	topped := outboxPayloads(t, pool, EventToppedUp)
	if len(topped) != 1 || topped[0]["topup_id"] != created.ID || topped[0]["owner_type"] != "rider" ||
		topped[0]["amount"] != "5000" || topped[0]["balance"] != "12000" {
		t.Fatalf("top-ups %v", topped)
	}
}

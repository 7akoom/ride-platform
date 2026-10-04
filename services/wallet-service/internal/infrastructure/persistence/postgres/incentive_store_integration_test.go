package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/earnings"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/incentives"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

const (
	incentiveDriverA = "d1d1d1d1-d1d1-4d1d-8d1d-d1d1d1d1d1d1"
	incentiveDriverB = "d2d2d2d2-d2d2-4d2d-8d2d-d2d2d2d2d2d2"
)

func campaignFixture(name string, start, end time.Time) incentives.Campaign {
	rate := decimal.NewFromInt(80)

	return incentives.Campaign{
		Name: name, CurrencyCode: "IQD", StartsAt: start, EndsAt: end,
		ZoneIDs: []string{"11111111-1111-4111-8111-111111111111"}, VehicleClass: "economy",
		DailyStart: 22 * 60, DailyEnd: 2 * 60, TimeZone: "Asia/Baghdad",
		MinAcceptanceRate: &rate,
		Tiers: []incentives.Tier{
			{Trips: 2, Amount: decimal.NewFromInt(5000)},
			{Trips: 5, Amount: decimal.NewFromInt(15000)},
		},
	}
}

func incentiveBalance(t *testing.T, repo *WalletRepository, driverID string) string {
	t.Helper()

	w, err := repo.FindWallet(context.Background(), wallet.OwnerDriver, driverID)
	if err != nil {
		t.Fatal(err)
	}

	return w.Balance.String()
}

func TestIncentiveCampaignIsSettledOncePerDriver(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewWalletRepository(pool)
	store := NewIncentiveStore(repo)
	now := time.Now().UTC()

	ended := campaignFixture("Night quest", now.Add(-48*time.Hour), now.Add(-time.Hour))

	created, err := store.Create(ctx, ended, "", "key-1")
	if err != nil {
		t.Fatal(err)
	}

	again, err := store.Create(ctx, campaignFixture("Other", now.Add(time.Hour), now.Add(2*time.Hour)), "", "key-1")
	if err != nil || again.ID != created.ID {
		t.Fatalf("a reused key should return the first campaign: %v %v", again.ID, err)
	}

	if created.DailyStart != 22*60 || created.DailyEnd != 2*60 || len(created.Tiers) != 2 ||
		!created.Tiers[1].Amount.Equal(decimal.NewFromInt(15000)) || len(created.ZoneIDs) != 1 ||
		created.MinAcceptanceRate == nil || created.MaxCancellationRate != nil {
		t.Fatalf("campaign read back wrong: %+v", created)
	}

	if created.StateAt(now) != incentives.StateSettling {
		t.Fatalf("an ended campaign should be settling, got %s", created.StateAt(now))
	}

	if _, err := store.Cancel(ctx, created.ID, "late", now); !errors.Is(err, incentives.ErrNotCancellable) {
		t.Fatalf("an ended campaign must not be cancellable: %v", err)
	}

	claimed, found, err := store.ClaimDue(ctx, now, time.Minute)
	if err != nil || !found || claimed.ID != created.ID || claimed.Status != incentives.StatusSettling {
		t.Fatalf("claim: %+v %v %v", claimed, found, err)
	}

	if _, found, _ := store.ClaimDue(ctx, now, time.Minute); found {
		t.Fatal("a leased campaign must not be claimed twice")
	}

	paid := claimed.Decide(incentives.Activity{DriverID: incentiveDriverA, CompletedTrips: 6, OffersAccepted: 9, OffersDeclined: 1}, nil)
	if paid.Status != incentives.PayoutPaid || !paid.Amount.Equal(decimal.NewFromInt(15000)) {
		t.Fatalf("decide: %+v", paid)
	}

	saved, err := store.Settle(ctx, claimed, paid)
	if err != nil || saved.Status != incentives.PayoutPaid {
		t.Fatalf("settle: %+v %v", saved, err)
	}

	if _, err := store.Settle(ctx, claimed, paid); err != nil {
		t.Fatalf("settling again: %v", err)
	}

	if got := incentiveBalance(t, repo, incentiveDriverA); got != "15000" {
		t.Fatalf("driver should be paid once, balance %s", got)
	}

	rating := decimal.RequireFromString("4.9")
	refused := claimed.Decide(incentives.Activity{DriverID: incentiveDriverB, CompletedTrips: 3, OffersAccepted: 1, OffersDeclined: 9}, &rating)

	if _, err := store.Settle(ctx, claimed, refused); err != nil {
		t.Fatal(err)
	}

	if err := store.MarkSettled(ctx, claimed.ID, now); err != nil {
		t.Fatal(err)
	}

	settled, err := store.Get(ctx, claimed.ID)
	if err != nil || settled.Status != incentives.StatusSettled || settled.PaidDrivers != 1 ||
		!settled.PaidTotal.Equal(decimal.NewFromInt(15000)) {
		t.Fatalf("settled campaign: %+v %v", settled, err)
	}

	payouts, err := store.Payouts(ctx, claimed.ID, "", 10)
	if err != nil || len(payouts) != 2 {
		t.Fatalf("payouts: %+v %v", payouts, err)
	}

	b := payouts[1]
	if b.DriverID != incentiveDriverB || b.Status != incentives.PayoutNotEligible || b.TierTrips != 2 ||
		len(b.Unmet) != 1 || b.Unmet[0] != incentives.UnmetAcceptance || b.Rating == nil || !b.Rating.Equal(rating) {
		t.Fatalf("not eligible payout: %+v", b)
	}

	mine, err := store.DriverPayouts(ctx, incentiveDriverA, []string{claimed.ID})
	if err != nil || mine[claimed.ID].Status != incentives.PayoutPaid {
		t.Fatalf("driver payouts: %+v %v", mine, err)
	}

	var events int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM outbox_events WHERE event_type = 'wallet.incentive_paid' AND aggregate_id = $1`,
		claimed.ID).Scan(&events); err != nil || events != 1 {
		t.Fatalf("one wallet.incentive_paid event expected, got %d %v", events, err)
	}

	// A future campaign can be cancelled, and the states filter.
	future, err := store.Create(ctx, campaignFixture("Weekend", now.Add(time.Hour), now.Add(48*time.Hour)), "", "")
	if err != nil {
		t.Fatal(err)
	}

	scheduled, err := store.List(ctx, incentives.CampaignsQuery{Status: string(incentives.StateScheduled), Limit: 10, Now: now})
	if err != nil || len(scheduled) != 1 || scheduled[0].ID != future.ID {
		t.Fatalf("scheduled list: %+v %v", scheduled, err)
	}

	visible, err := store.Visible(ctx, now.Add(-24*time.Hour))
	if err != nil || len(visible) != 2 {
		t.Fatalf("visible: %d %v", len(visible), err)
	}

	cancelled, err := store.Cancel(ctx, future.ID, "budget", now)
	if err != nil || cancelled.Status != incentives.StatusCancelled || cancelled.CancelReason != "budget" {
		t.Fatalf("cancel: %+v %v", cancelled, err)
	}

	all, err := store.List(ctx, incentives.CampaignsQuery{Limit: 1, Now: now})
	if err != nil || len(all) != 1 {
		t.Fatalf("page: %v", err)
	}

	rest, err := store.List(ctx, incentives.CampaignsQuery{Limit: 10, AfterID: all[0].ID, Now: now})
	if err != nil || len(rest) != 1 || rest[0].ID == all[0].ID {
		t.Fatalf("next page: %+v %v", rest, err)
	}
}

func TestEarningsAreSummedByLocalDay(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewWalletRepository(pool)

	baghdad, err := time.LoadLocation("Asia/Baghdad")
	if err != nil {
		t.Fatal(err)
	}

	// 2026-09-30 23:30 Baghdad is 20:30 UTC, still the 30th locally; 01:00 on
	// the 1st locally is 22:00 UTC on the 30th.
	late := time.Date(2026, 9, 30, 23, 30, 0, 0, baghdad)
	early := time.Date(2026, 10, 1, 1, 0, 0, 0, baghdad)

	settle := func(trip string, at time.Time, kind, method string, fare, commission, earning, cash, walletPaid int64) {
		if _, err := pool.Exec(ctx,
			`INSERT INTO trip_settlements
			    (trip_id, rider_id, driver_id, currency_code, payment_method, fare_amount, commission_rate,
			     commission_amount, driver_earning, cash_amount, wallet_amount, kind, created_at)
			 VALUES ($1, $2, $3, 'IQD', $4, $5, 10, $6, $7, $8, $9, $10, $11)`,
			trip, riderA, incentiveDriverA, method, fare, commission, earning, cash, walletPaid, kind, at); err != nil {
			t.Fatal(err)
		}
	}

	settle("10000000-0000-4000-8000-000000000001", late, "trip", "cash", 10000, 1000, 9000, 10000, 0)
	settle("10000000-0000-4000-8000-000000000002", early, "trip", "wallet", 6000, 600, 5400, 0, 6000)

	if _, err := pool.Exec(ctx, `INSERT INTO trip_settlements
		    (trip_id, rider_id, driver_id, currency_code, payment_method, fare_amount, commission_rate,
		     commission_amount, driver_earning, kind, created_at)
		 VALUES ('10000000-0000-4000-8000-000000000003', $1, $2, 'IQD', 'cash', 2000, 0, 0, 2000, 'cancellation', $3)`,
		riderA, incentiveDriverA, early); err != nil {
		t.Fatal(err)
	}

	if _, _, err := repo.ApplyMovement(ctx, wallet.MovementInput{
		OwnerType: wallet.OwnerDriver, OwnerID: incentiveDriverA, Type: wallet.TxIncentive,
		Amount: decimal.NewFromInt(7000), IdempotencyKey: "incentive:test",
	}); err != nil {
		t.Fatal(err)
	}

	// Paid on the 1st, inside the week below, whatever day the test runs.
	if _, err := pool.Exec(ctx, `UPDATE wallet_transactions SET created_at = $1 WHERE idempotency_key = 'incentive:test'`, early); err != nil {
		t.Fatal(err)
	}

	service := earnings.NewService(NewEarningsStore(repo), baghdad)

	day, err := service.Get(ctx, incentiveDriverA, earnings.PeriodDay, "2026-09-30")
	if err != nil {
		t.Fatal(err)
	}

	if day.Totals.Trips != 1 || !day.Totals.Fares.Equal(decimal.NewFromInt(10000)) ||
		!day.Totals.CashCollected.Equal(decimal.NewFromInt(10000)) || !day.Totals.Commission.Equal(decimal.NewFromInt(1000)) {
		t.Fatalf("30th: %+v", day.Totals)
	}

	week, err := service.Get(ctx, incentiveDriverA, earnings.PeriodWeek, "2026-10-01")
	if err != nil {
		t.Fatal(err)
	}

	if week.FromDate != "2026-09-28" || week.ToDate != "2026-10-04" || len(week.Days) != 7 || week.Totals.Trips != 2 ||
		!week.Totals.WalletPaid.Equal(decimal.NewFromInt(6000)) || !week.Totals.TripEarnings.Equal(decimal.NewFromInt(14400)) ||
		!week.Totals.Fees.Equal(decimal.NewFromInt(2000)) || !week.Totals.Net().Equal(decimal.NewFromInt(16400+7000)) {
		t.Fatalf("week: %+v", week)
	}

	if week.Days[2].Totals.Trips != 1 || week.Days[3].Totals.Trips != 1 {
		t.Fatalf("days split wrong: %+v", week.Days)
	}

	month, err := service.Get(ctx, incentiveDriverA, earnings.PeriodMonth, "2026-10-15")
	if err != nil {
		t.Fatal(err)
	}

	if !month.Totals.Incentives.Equal(decimal.NewFromInt(7000)) {
		t.Fatalf("October's incentives: %+v", month.Totals)
	}
}

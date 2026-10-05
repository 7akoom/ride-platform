package query

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/analytics/internal/domain"
)

func d(v string) decimal.Decimal { return decimal.RequireFromString(v) }

func TestSumMoneyFlows(t *testing.T) {
	day1, _ := time.Parse(time.DateOnly, "2026-10-01")
	day2, _ := time.Parse(time.DateOnly, "2026-10-02")

	flows := SumMoneyFlows(domain.Ledger{
		Currency: "IQD",
		Days: []domain.LedgerDay{
			{Day: day1, Lines: []domain.LedgerLine{
				{OwnerType: "rider", Type: "top_up", Entries: 2, Credited: d("7000")},
				{OwnerType: "driver", Type: "commission", Entries: 3, Debited: d("900")},
				{OwnerType: "driver", Type: "payout", Entries: 1, Debited: d("5000")},
			}},
			{Day: day2, Lines: []domain.LedgerLine{
				{OwnerType: "rider", Type: "top_up", Entries: 1, Credited: d("1000")},
				{OwnerType: "driver", Type: "payout_return", Entries: 1, Credited: d("5000")},
				{OwnerType: "driver", Type: "payout", Entries: 1, Debited: d("3000")},
				{OwnerType: "rider", Type: "voucher", Entries: 1, Credited: d("10000")},
				{OwnerType: "driver", Type: "tip", Entries: 1, Credited: d("500")},
				{OwnerType: "rider", Type: "refund", Entries: 1, Credited: d("700")},
				{OwnerType: "driver", Type: "refund", Entries: 1, Debited: d("200")},
				{OwnerType: "rider", Type: "adjustment", Entries: 2, Credited: d("100"), Debited: d("40")},
				{OwnerType: "rider", Type: "transfer_out", Entries: 1, Debited: d("2500")},
				{OwnerType: "driver", Type: "incentive", Entries: 1, Credited: d("15000")},
			}},
		},
	})

	h := flows.Headline
	for name, pair := range map[string][2]decimal.Decimal{
		"topped up":   {h.ToppedUp, d("8000")},
		"vouchers":    {h.VouchersRedeemed, d("10000")},
		"commission":  {h.Commission, d("900")},
		"tips":        {h.Tips, d("500")},
		"refunds":     {h.Refunds, d("700")},
		"incentives":  {h.Incentives, d("15000")},
		"paid out":    {h.PaidOut, d("3000")},
		"transferred": {h.Transferred, d("2500")},
		"adjust in":   {h.AdjustmentsIn, d("100")},
		"adjust out":  {h.AdjustmentsOut, d("40")},
	} {
		if !pair[0].Equal(pair[1]) {
			t.Errorf("%s = %s, want %s", name, pair[0], pair[1])
		}
	}

	if len(flows.Totals) != 11 || flows.Totals[0].OwnerType != "driver" || flows.Currency != "IQD" {
		t.Fatalf("totals %+v", flows.Totals)
	}

	for _, line := range flows.Totals {
		if line.OwnerType == "rider" && line.Type == "top_up" && (line.Entries != 3 || !line.Credited.Equal(d("8000"))) {
			t.Fatalf("rider top-ups %+v", line)
		}
	}
}

func TestCountDrivers(t *testing.T) {
	supply := []domain.DriverSupply{
		{Status: "active", Availability: "available", VehicleClass: "economy", Drivers: 4},
		{Status: "active", Availability: "busy", VehicleClass: "economy", Drivers: 2},
		{Status: "active", Availability: "offline", VehicleClass: "comfort", Drivers: 3},
		{Status: "active", Availability: "available", VehicleClass: "comfort", Drivers: 1},
		{Status: "pending", Availability: "offline", VehicleClass: "economy", Drivers: 5},
		{Status: "suspended", Availability: "offline", VehicleClass: "economy", Drivers: 9},
		{Status: "rejected", Availability: "offline", VehicleClass: "comfort", Drivers: 1},
	}

	var all domain.LiveOverview
	CountDrivers(&all, supply, "")

	if all.Available != 5 || all.Busy != 2 || all.Offline != 3 || all.PendingReview != 5 || len(all.ByClass) != 2 ||
		all.ByClass[0].VehicleClass != "comfort" || all.ByClass[1].Available != 4 {
		t.Fatalf("all %+v", all)
	}

	var comfort domain.LiveOverview
	CountDrivers(&comfort, supply, "comfort")

	if comfort.Available != 1 || comfort.Offline != 3 || comfort.PendingReview != 0 || len(comfort.ByClass) != 1 {
		t.Fatalf("comfort %+v", comfort)
	}
}

func TestRates(t *testing.T) {
	if got := AcceptanceRate(domain.OfferDay{Offered: 10, Accepted: 3, Rejected: 2, Expired: 1, Pending: 4}).StringFixed(2); got != "50.00" {
		t.Fatalf("acceptance %s", got)
	}

	if got := CompletionRate(domain.ServiceLevelDay{}).StringFixed(2); got != "0.00" {
		t.Fatalf("nothing requested %s", got)
	}
}

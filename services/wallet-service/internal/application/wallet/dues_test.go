package wallet_test

import (
	"context"
	"testing"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

func TestARiderWhoOwesFeesMayNotRequestTrips(t *testing.T) {
	repo := newFakeRepository()
	repo.config.CurrencyCode = "IQD"
	repo.config.BlockTripsWithDues = true
	repo.dues = []wallet.Due{
		{TripID: "trip-1", Kind: wallet.SettlementCancellation, Amount: d("1000"), Paid: d("400")},
		{TripID: "trip-2", Kind: wallet.SettlementNoShow, Amount: d("2000"), Paid: d("0")},
	}

	service := wallet.NewService(repo)

	dues, err := service.RiderDues(context.Background(), "rider-1")
	if err != nil || !dues.Outstanding.Equal(d("2600")) || dues.CanRequestTrips || len(dues.Dues) != 2 {
		t.Fatalf("dues %+v %v", dues, err)
	}

	repo.config.BlockTripsWithDues = false
	if dues, _ := service.RiderDues(context.Background(), "rider-1"); !dues.CanRequestTrips {
		t.Fatal("a deployment that does not block lets them")
	}

	repo.config.BlockTripsWithDues = true
	repo.dues = nil
	if dues, _ := service.RiderDues(context.Background(), "rider-1"); !dues.CanRequestTrips || !dues.Outstanding.IsZero() {
		t.Fatalf("nothing owed: %+v", dues)
	}

	if _, err := service.RiderDues(context.Background(), " "); err == nil {
		t.Fatal("a rider is required")
	}
}

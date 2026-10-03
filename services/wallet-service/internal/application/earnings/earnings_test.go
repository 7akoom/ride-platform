package earnings

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

type fakeStore struct {
	from, to time.Time
	zone     string
	days     []Day
}

func (f *fakeStore) Config(context.Context) (wallet.Config, error) {
	return wallet.Config{CurrencyCode: "IQD"}, nil
}

func (f *fakeStore) Days(_ context.Context, _ string, from, to time.Time, zone string) ([]Day, error) {
	f.from, f.to, f.zone = from, to, zone

	return f.days, nil
}

const driver = "dddddddd-dddd-4ddd-8ddd-dddddddddddd"

func totals(trips int, earned int64) Totals {
	t := zero()
	t.Trips = trips
	t.TripEarnings = decimal.NewFromInt(earned)

	return t
}

func service(t *testing.T, store *fakeStore) *Service {
	t.Helper()

	loc, err := time.LoadLocation("Asia/Baghdad")
	if err != nil {
		t.Fatal(err)
	}

	s := NewService(store, loc)
	s.now = func() time.Time { return time.Date(2026, 10, 3, 22, 30, 0, 0, time.UTC) } // the 4th in Baghdad

	return s
}

func TestTodayIsTheLocalDay(t *testing.T) {
	store := &fakeStore{}

	report, err := service(t, store).Get(context.Background(), driver, "", "")
	if err != nil {
		t.Fatal(err)
	}

	if report.FromDate != "2026-10-04" || report.ToDate != "2026-10-04" || len(report.Days) != 0 ||
		store.zone != "Asia/Baghdad" || store.to.Sub(store.from) != 24*time.Hour || report.CurrencyCode != "IQD" {
		t.Fatalf("report: %+v, store %+v", report, store)
	}
}

func TestAWeekRunsMondayToSundayWithEveryDay(t *testing.T) {
	store := &fakeStore{days: []Day{
		{Date: "2026-09-28", Totals: totals(2, 9000)},
		{Date: "2026-10-04", Totals: totals(1, 3000)},
	}}

	report, err := service(t, store).Get(context.Background(), driver, PeriodWeek, "2026-10-04")
	if err != nil {
		t.Fatal(err)
	}

	if report.FromDate != "2026-09-28" || report.ToDate != "2026-10-04" || len(report.Days) != 7 ||
		report.Totals.Trips != 3 || !report.Totals.Net().Equal(decimal.NewFromInt(12000)) || report.Days[3].Totals.Trips != 0 {
		t.Fatalf("week: %+v", report)
	}
}

func TestAMonthListsAllItsDays(t *testing.T) {
	report, err := service(t, &fakeStore{}).Get(context.Background(), driver, PeriodMonth, "2026-02-14")
	if err != nil {
		t.Fatal(err)
	}

	if report.FromDate != "2026-02-01" || report.ToDate != "2026-02-28" || len(report.Days) != 28 {
		t.Fatalf("month: %+v", report)
	}
}

func TestBadRequests(t *testing.T) {
	s := service(t, &fakeStore{})

	if _, err := s.Get(context.Background(), "x", PeriodDay, ""); !errors.Is(err, ErrDriverNotFound) {
		t.Fatal(err)
	}

	if _, err := s.Get(context.Background(), driver, "year", ""); !errors.Is(err, ErrInvalidPeriod) {
		t.Fatal(err)
	}

	if _, err := s.Get(context.Background(), driver, PeriodDay, "03/10/2026"); !errors.Is(err, ErrInvalidDate) {
		t.Fatal(err)
	}
}

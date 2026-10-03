// Package earnings sums what a driver earned in a day, week or month: trips
// (fares, commission, cash and wallet), fees, tips, incentives, refunds
// charged and adjustments.
package earnings

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

type Period string

const (
	PeriodDay   Period = "day"
	PeriodWeek  Period = "week"
	PeriodMonth Period = "month"

	dateLayout = "2006-01-02"
)

var (
	ErrDriverNotFound = errors.New("driver not found")
	ErrInvalidPeriod  = errors.New("period must be day, week or month")
	ErrInvalidDate    = errors.New("date must be YYYY-MM-DD")
)

var uuidShape = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Totals are the sums of one day or more.
type Totals struct {
	Trips         int
	Fares         wallet.Money
	Commission    wallet.Money
	TripEarnings  wallet.Money
	Fees          wallet.Money
	Tips          wallet.Money
	Incentives    wallet.Money
	Refunds       wallet.Money
	Adjustments   wallet.Money
	CashCollected wallet.Money
	WalletPaid    wallet.Money
}

// Net is what the driver earned: trips, fees, tips and incentives, less
// refunds charged to them, with staff adjustments.
func (t Totals) Net() wallet.Money {
	return t.TripEarnings.Add(t.Fees).Add(t.Tips).Add(t.Incentives).Add(t.Refunds).Add(t.Adjustments)
}

func (t Totals) plus(o Totals) Totals {
	return Totals{
		Trips:         t.Trips + o.Trips,
		Fares:         t.Fares.Add(o.Fares),
		Commission:    t.Commission.Add(o.Commission),
		TripEarnings:  t.TripEarnings.Add(o.TripEarnings),
		Fees:          t.Fees.Add(o.Fees),
		Tips:          t.Tips.Add(o.Tips),
		Incentives:    t.Incentives.Add(o.Incentives),
		Refunds:       t.Refunds.Add(o.Refunds),
		Adjustments:   t.Adjustments.Add(o.Adjustments),
		CashCollected: t.CashCollected.Add(o.CashCollected),
		WalletPaid:    t.WalletPaid.Add(o.WalletPaid),
	}
}

func zero() Totals {
	z := decimal.Zero

	return Totals{Fares: z, Commission: z, TripEarnings: z, Fees: z, Tips: z, Incentives: z,
		Refunds: z, Adjustments: z, CashCollected: z, WalletPaid: z}
}

// Day is one local calendar day's totals.
type Day struct {
	Date   string
	Totals Totals
}

// Report is a driver's earnings for a period.
type Report struct {
	DriverID     string
	CurrencyCode string
	Period       Period
	FromDate     string
	ToDate       string
	TimeZone     string
	Totals       Totals
	// Days lists every day of a week or month, also those without earnings.
	Days []Day
}

// Store sums a driver's earnings between from and to, by local day.
type Store interface {
	Config(ctx context.Context) (wallet.Config, error)
	// Days returns only the days with something on them.
	Days(ctx context.Context, driverID string, from, to time.Time, timeZone string) ([]Day, error)
}

type Service struct {
	store    Store
	location *time.Location
	now      func() time.Time
}

func NewService(store Store, location *time.Location) *Service {
	if store == nil || location == nil {
		panic("earnings store and time zone are required")
	}

	return &Service{store: store, location: location, now: func() time.Time { return time.Now() }}
}

// Get reports the period around date (today when empty). A week runs Monday
// to Sunday.
func (s *Service) Get(ctx context.Context, driverID string, period Period, date string) (Report, error) {
	driverID = strings.ToLower(strings.TrimSpace(driverID))
	if !uuidShape.MatchString(driverID) {
		return Report{}, ErrDriverNotFound
	}

	if period == "" {
		period = PeriodDay
	}

	day := s.now().In(s.location)

	if date = strings.TrimSpace(date); date != "" {
		parsed, err := time.ParseInLocation(dateLayout, date, s.location)
		if err != nil {
			return Report{}, ErrInvalidDate
		}

		day = parsed
	}

	first := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, s.location)

	var end time.Time

	switch period {
	case PeriodDay:
		end = first.AddDate(0, 0, 1)
	case PeriodWeek:
		back := (int(first.Weekday()) + 6) % 7
		first = first.AddDate(0, 0, -back)
		end = first.AddDate(0, 0, 7)
	case PeriodMonth:
		first = time.Date(first.Year(), first.Month(), 1, 0, 0, 0, 0, s.location)
		end = first.AddDate(0, 1, 0)
	default:
		return Report{}, ErrInvalidPeriod
	}

	config, err := s.store.Config(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("read wallet config: %w", err)
	}

	found, err := s.store.Days(ctx, driverID, first, end, s.location.String())
	if err != nil {
		return Report{}, fmt.Errorf("sum earnings: %w", err)
	}

	byDate := make(map[string]Totals, len(found))
	for _, d := range found {
		byDate[d.Date] = d.Totals
	}

	report := Report{
		DriverID:     driverID,
		CurrencyCode: config.CurrencyCode,
		Period:       period,
		FromDate:     first.Format(dateLayout),
		ToDate:       end.AddDate(0, 0, -1).Format(dateLayout),
		TimeZone:     s.location.String(),
		Totals:       zero(),
	}

	for d := first; d.Before(end); d = d.AddDate(0, 0, 1) {
		key := d.Format(dateLayout)

		totals, ok := byDate[key]
		if !ok {
			totals = zero()
		}

		report.Totals = report.Totals.plus(totals)

		if period != PeriodDay {
			report.Days = append(report.Days, Day{Date: key, Totals: totals})
		}
	}

	return report, nil
}

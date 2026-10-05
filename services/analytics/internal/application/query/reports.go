package query

import (
	"context"
	"sort"
	"time"

	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/analytics/internal/domain"
)

// Ledger sums the wallets' ledger (wallet-service).
type Ledger interface {
	Summarize(ctx context.Context, start, end time.Time, timeZone string) (domain.Ledger, error)
}

// Drivers counts drivers now (driver-service).
type Drivers interface {
	Supply(ctx context.Context) ([]domain.DriverSupply, error)
}

// WithLedger and WithDrivers give the reports that read other services.
func (s *Service) WithLedger(ledger Ledger) *Service {
	s.ledger = ledger

	return s
}

func (s *Service) WithDrivers(drivers Drivers) *Service {
	s.drivers = drivers

	return s
}

func (s *Service) ServiceLevels(ctx context.Context, rng domain.DateRange, scope domain.Scope) ([]domain.ServiceLevelDay, domain.ServiceLevelDay, domain.Window, error) {
	w, err := s.window(ctx, rng, scope)
	if err != nil {
		return nil, domain.ServiceLevelDay{}, w, err
	}

	days, totals, err := s.reader.ServiceLevels(ctx, w, scope)

	return days, totals, w, err
}

func (s *Service) DriverOffers(ctx context.Context, rng domain.DateRange, scope domain.Scope) ([]domain.OfferDay, domain.OfferDay, domain.Window, error) {
	w, err := s.window(ctx, rng, scope)
	if err != nil {
		return nil, domain.OfferDay{}, w, err
	}

	days, totals, err := s.reader.DriverOffers(ctx, w, scope, s.now())

	return days, totals, w, err
}

func (s *Service) Ratings(ctx context.Context, rng domain.DateRange, scope domain.Scope) (domain.Ratings, domain.Window, error) {
	w, err := s.window(ctx, rng, scope)
	if err != nil {
		return domain.Ratings{}, w, err
	}

	out, err := s.reader.Ratings(ctx, w, scope)

	return out, w, err
}

// MoneyFlows is the whole platform's ledger on the platform's clock.
func (s *Service) MoneyFlows(ctx context.Context, rng domain.DateRange) (domain.MoneyFlows, domain.Window, error) {
	w, err := ResolveWindow(rng, s.location, s.now())
	if err != nil {
		return domain.MoneyFlows{}, w, err
	}

	if s.ledger == nil {
		return domain.MoneyFlows{}, w, domain.ErrUpstreamUnavailable
	}

	ledger, err := s.ledger.Summarize(ctx, w.Start, w.End, w.Location.String())
	if err != nil {
		return domain.MoneyFlows{}, w, err
	}

	return SumMoneyFlows(ledger), w, nil
}

// SumMoneyFlows adds up the ledger's days into totals and the headline.
func SumMoneyFlows(ledger domain.Ledger) domain.MoneyFlows {
	out := domain.MoneyFlows{Days: ledger.Days, Held: ledger.Held, Currency: ledger.Currency}
	totals := map[string]*domain.LedgerLine{}

	for _, day := range ledger.Days {
		for _, line := range day.Lines {
			key := line.OwnerType + "/" + line.Type

			total, ok := totals[key]
			if !ok {
				total = &domain.LedgerLine{OwnerType: line.OwnerType, Type: line.Type}
				totals[key] = total
			}

			total.Entries += line.Entries
			total.Credited = total.Credited.Add(line.Credited)
			total.Debited = total.Debited.Add(line.Debited)
		}
	}

	keys := make([]string, 0, len(totals))
	for key := range totals {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	credited := func(owner, kind string) decimal.Decimal {
		var sum decimal.Decimal

		for _, line := range totals {
			if line.Type == kind && (owner == "" || line.OwnerType == owner) {
				sum = sum.Add(line.Credited)
			}
		}

		return sum
	}

	debited := func(owner, kind string) decimal.Decimal {
		var sum decimal.Decimal

		for _, line := range totals {
			if line.Type == kind && (owner == "" || line.OwnerType == owner) {
				sum = sum.Add(line.Debited)
			}
		}

		return sum
	}

	for _, key := range keys {
		out.Totals = append(out.Totals, *totals[key])
	}

	out.Headline = domain.MoneyHeadline{
		ToppedUp:         credited("", "top_up"),
		VouchersRedeemed: credited("", "voucher"),
		Commission:       debited("driver", "commission").Sub(credited("driver", "commission")),
		Tips:             credited("driver", "tip"),
		Refunds:          credited("rider", "refund"),
		Incentives:       credited("driver", "incentive"),
		PaidOut:          debited("driver", "payout").Sub(credited("driver", "payout_return")),
		Transferred:      debited("rider", "transfer_out"),
		AdjustmentsIn:    credited("", "adjustment"),
		AdjustmentsOut:   debited("", "adjustment"),
	}

	return out
}

// LiveOverview is now: trips under way and today so far (scope applies,
// on the report's clock), and drivers by state (the platform's).
func (s *Service) LiveOverview(ctx context.Context, scope domain.Scope) (domain.LiveOverview, *time.Location, error) {
	w, err := s.window(ctx, domain.DateRange{}, scope)
	if err != nil {
		return domain.LiveOverview{}, s.location, err
	}

	today := w
	today.From = w.To
	today.Start = time.Date(w.To.Year(), w.To.Month(), w.To.Day(), 0, 0, 0, 0, w.Location)

	now := s.now()
	out := domain.LiveOverview{AsOf: now.UTC()}

	if out.Trips, err = s.reader.LiveTrips(ctx, scope, now); err != nil {
		return out, w.Location, err
	}

	if _, out.Today, err = s.reader.TripFunnel(ctx, today, scope); err != nil {
		return out, w.Location, err
	}

	out.Today.Day = today.From

	_, revenue, err := s.reader.Revenue(ctx, today, scope)
	if err != nil {
		return out, w.Location, err
	}

	out.TodayGrossFare, out.Currency = revenue.GrossFareTotal, revenue.Currency

	if s.drivers == nil {
		return out, w.Location, domain.ErrUpstreamUnavailable
	}

	supply, err := s.drivers.Supply(ctx)
	if err != nil {
		return out, w.Location, err
	}

	CountDrivers(&out, supply, scope.VehicleClass)

	return out, w.Location, nil
}

// CountDrivers fills the driver figures: approved (active) drivers by
// availability and class, and those waiting for review; vehicleClass, when
// set, keeps one class.
func CountDrivers(out *domain.LiveOverview, supply []domain.DriverSupply, vehicleClass string) {
	byClass := map[string]*domain.ClassSupply{}

	for _, c := range supply {
		if vehicleClass != "" && c.VehicleClass != vehicleClass {
			continue
		}

		if c.Status == "pending" {
			out.PendingReview += c.Drivers

			continue
		}

		if c.Status != "active" {
			continue
		}

		class, ok := byClass[c.VehicleClass]
		if !ok {
			class = &domain.ClassSupply{VehicleClass: c.VehicleClass}
			byClass[c.VehicleClass] = class
		}

		switch c.Availability {
		case "available":
			class.Available += c.Drivers
			out.Available += c.Drivers
		case "busy":
			class.Busy += c.Drivers
			out.Busy += c.Drivers
		default:
			class.Offline += c.Drivers
			out.Offline += c.Drivers
		}
	}

	classes := make([]string, 0, len(byClass))
	for class := range byClass {
		classes = append(classes, class)
	}

	sort.Strings(classes)

	for _, class := range classes {
		out.ByClass = append(out.ByClass, *byClass[class])
	}
}

// AcceptanceRate is accepted over offers that ended.
func AcceptanceRate(o domain.OfferDay) decimal.Decimal {
	return ratio(o.Accepted, o.Accepted+o.Rejected+o.Expired)
}

// CompletionRate is completed over requested.
func CompletionRate(d domain.ServiceLevelDay) decimal.Decimal {
	return ratio(d.Completed, d.Requested)
}

func ratio(part, whole int64) decimal.Decimal {
	if whole == 0 {
		return decimal.Zero
	}

	return decimal.NewFromInt(part).Mul(decimal.NewFromInt(100)).DivRound(decimal.NewFromInt(whole), 2)
}

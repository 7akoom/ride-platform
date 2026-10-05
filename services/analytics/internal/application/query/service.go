package query

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"

	"github.com/7akoom/ride-platform/services/analytics/internal/domain"
	"github.com/7akoom/ride-platform/services/analytics/internal/infrastructure/postgres"
)

const (
	defaultRangeDays   = 30
	maxRangeDays       = 366
	defaultCohortWeeks = 12
	maxCohortWeeks     = 52
)

var vehicleClassPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

// Cities gives a city's IANA time zone; domain.ErrUnknownCity when there is
// no such city, domain.ErrUpstreamUnavailable when it cannot tell.
type Cities interface {
	TimeZone(ctx context.Context, cityID string) (string, error)
}

// Service validates and resolves report requests; the Reader runs them.
type Service struct {
	reader   *postgres.Reader
	cities   Cities
	location *time.Location
	now      func() time.Time
}

// NewService: location is the platform's clock (ANALYTICS_TIME_ZONE), used
// when a report is not about one city.
func NewService(reader *postgres.Reader, cities Cities, location *time.Location) *Service {
	if cities == nil || location == nil {
		panic("analytics query service needs cities and a time zone")
	}

	return &Service{reader: reader, cities: cities, location: location, now: time.Now}
}

func (s *Service) TripFunnel(ctx context.Context, rng domain.DateRange, scope domain.Scope) ([]domain.FunnelDayPoint, domain.FunnelDayPoint, domain.Window, error) {
	w, err := s.window(ctx, rng, scope)
	if err != nil {
		return nil, domain.FunnelDayPoint{}, w, err
	}

	days, totals, err := s.reader.TripFunnel(ctx, w, scope)

	return days, totals, w, err
}

func (s *Service) Cancellations(ctx context.Context, rng domain.DateRange, scope domain.Scope) (domain.CancellationBreakdown, domain.Window, error) {
	w, err := s.window(ctx, rng, scope)
	if err != nil {
		return domain.CancellationBreakdown{}, w, err
	}

	out, err := s.reader.Cancellations(ctx, w, scope)

	return out, w, err
}

func (s *Service) Revenue(ctx context.Context, rng domain.DateRange, scope domain.Scope) ([]domain.RevenueDayPoint, domain.RevenueSummary, domain.Window, error) {
	w, err := s.window(ctx, rng, scope)
	if err != nil {
		return nil, domain.RevenueSummary{}, w, err
	}

	days, summary, err := s.reader.Revenue(ctx, w, scope)

	return days, summary, w, err
}

func (s *Service) RiderRetention(ctx context.Context, cohortWeeks int32) ([]domain.RetentionCohort, *time.Location, error) {
	first, this, err := s.weeks(cohortWeeks)
	if err != nil {
		return nil, s.location, err
	}

	cohorts, err := s.reader.RiderRetention(ctx, s.location, first, this)

	return cohorts, s.location, err
}

func (s *Service) DriverRetention(ctx context.Context, cohortWeeks int32) ([]domain.RetentionCohort, *time.Location, error) {
	first, this, err := s.weeks(cohortWeeks)
	if err != nil {
		return nil, s.location, err
	}

	cohorts, err := s.reader.DriverRetention(ctx, s.location, first, this)

	return cohorts, s.location, err
}

// window checks the scope, picks the clock (the city's, else the
// platform's) and turns the range into local days and instants.
func (s *Service) window(ctx context.Context, rng domain.DateRange, scope domain.Scope) (domain.Window, error) {
	if err := validateScope(scope); err != nil {
		return domain.Window{}, err
	}

	loc := s.location

	if scope.CityID != "" {
		zone, err := s.cities.TimeZone(ctx, scope.CityID)
		if err != nil {
			return domain.Window{}, err
		}

		if loc, err = time.LoadLocation(zone); err != nil {
			return domain.Window{}, fmt.Errorf("city %s has time zone %q: %w", scope.CityID, zone, err)
		}
	}

	return ResolveWindow(rng, loc, s.now())
}

// ResolveWindow turns a range into local days on loc: from/to dates first,
// then the day each timestamp falls on, else the last 30 days to today.
func ResolveWindow(rng domain.DateRange, loc *time.Location, now time.Time) (domain.Window, error) {
	today := civil(now.In(loc))

	day := func(text string, at time.Time, name string) (time.Time, bool, error) {
		if text != "" {
			parsed, err := time.Parse(time.DateOnly, text)
			if err != nil {
				return time.Time{}, false, fmt.Errorf("%w: %s must be a date (YYYY-MM-DD)", domain.ErrInvalidArgument, name)
			}

			return parsed, true, nil
		}

		if !at.IsZero() {
			return civil(at.In(loc)), true, nil
		}

		return time.Time{}, false, nil
	}

	to, hasTo, err := day(rng.To, rng.End, "to_date")
	if err != nil {
		return domain.Window{}, err
	}

	if !hasTo {
		to = today
	}

	from, hasFrom, err := day(rng.From, rng.Start, "from_date")
	if err != nil {
		return domain.Window{}, err
	}

	if !hasFrom {
		from = to.AddDate(0, 0, -(defaultRangeDays - 1))
	}

	if to.Before(from) {
		return domain.Window{}, fmt.Errorf("%w: the range ends before it starts", domain.ErrInvalidArgument)
	}

	w := domain.Window{From: from, To: to, Location: loc}
	if w.Days() > maxRangeDays {
		return domain.Window{}, fmt.Errorf("%w: a report covers at most %d days", domain.ErrInvalidArgument, maxRangeDays)
	}

	w.Start = time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, loc)
	next := to.AddDate(0, 0, 1)
	w.End = time.Date(next.Year(), next.Month(), next.Day(), 0, 0, 0, 0, loc)

	return w, nil
}

// weeks returns the first cohort's Monday and this week's Monday (local
// days on the platform's clock).
func (s *Service) weeks(cohortWeeks int32) (time.Time, time.Time, error) {
	switch {
	case cohortWeeks < 0 || cohortWeeks > maxCohortWeeks:
		return time.Time{}, time.Time{}, fmt.Errorf("%w: cohort_weeks is 1 to %d", domain.ErrInvalidArgument, maxCohortWeeks)
	case cohortWeeks == 0:
		cohortWeeks = defaultCohortWeeks
	}

	this := MondayOf(civil(s.now().In(s.location)))

	return this.AddDate(0, 0, -7*int(cohortWeeks-1)), this, nil
}

// MondayOf returns the Monday starting day's week.
func MondayOf(day time.Time) time.Time {
	back := (int(day.Weekday()) + 6) % 7

	return day.AddDate(0, 0, -back)
}

func validateScope(scope domain.Scope) error {
	for name, id := range map[string]string{"city_id": scope.CityID, "zone_id": scope.ZoneID} {
		if id == "" {
			continue
		}

		if _, err := uuid.Parse(id); err != nil {
			return fmt.Errorf("%w: %s must be a UUID", domain.ErrInvalidArgument, name)
		}
	}

	if scope.VehicleClass != "" && !vehicleClassPattern.MatchString(scope.VehicleClass) {
		return fmt.Errorf("%w: vehicle_class is not a vehicle class", domain.ErrInvalidArgument)
	}

	return nil
}

// civil is the local calendar day of t, as midnight UTC.
func civil(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

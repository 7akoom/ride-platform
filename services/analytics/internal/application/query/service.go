package query

import (
	"context"
	"fmt"
	"time"

	"github.com/7akoom/ride-platform/services/analytics/internal/domain"
	"github.com/7akoom/ride-platform/services/analytics/internal/infrastructure/postgres"
)

// defaultRangeDays is used whenever a caller passes a zero DateRange —
// keeps every endpoint usable without forcing the Admin web app to always
// compute an explicit range for a first-load dashboard view.
const defaultRangeDays = 30

// defaultCohortWeeks mirrors postgres.maxRetentionWeekOffset's practical
// default — how many past cohort weeks retention endpoints look at when
// the caller doesn't specify.
const defaultCohortWeeks = 12

// Service is the application-layer entry point the gRPC handler calls into.
// It owns input validation/defaulting; the Reader underneath assumes
// already-sane (start, end) bounds.
type Service struct {
	reader *postgres.Reader
}

func NewService(reader *postgres.Reader) *Service {
	return &Service{reader: reader}
}

func (s *Service) GetTripFunnel(ctx context.Context, rng domain.DateRange) ([]domain.FunnelDayPoint, domain.FunnelDayPoint, error) {
	start, end, err := resolveRange(rng)
	if err != nil {
		return nil, domain.FunnelDayPoint{}, err
	}

	return s.reader.GetTripFunnel(ctx, start, end)
}

func (s *Service) GetCancellationBreakdown(ctx context.Context, rng domain.DateRange) (domain.CancellationBreakdown, error) {
	start, end, err := resolveRange(rng)
	if err != nil {
		return domain.CancellationBreakdown{}, err
	}

	return s.reader.GetCancellationBreakdown(ctx, start, end)
}

func (s *Service) GetRevenueSummary(ctx context.Context, rng domain.DateRange) ([]domain.RevenueDayPoint, domain.RevenueSummary, error) {
	start, end, err := resolveRange(rng)
	if err != nil {
		return nil, domain.RevenueSummary{}, err
	}

	return s.reader.GetRevenueSummary(ctx, start, end)
}

func (s *Service) GetRiderRetention(ctx context.Context, cohortWeeks int32) ([]domain.RetentionCohort, error) {
	return s.reader.GetRiderRetention(ctx, resolveCohortWeeks(cohortWeeks))
}

func (s *Service) GetDriverRetention(ctx context.Context, cohortWeeks int32) ([]domain.RetentionCohort, error) {
	return s.reader.GetDriverRetention(ctx, resolveCohortWeeks(cohortWeeks))
}

// resolveRange fills in a defaultRangeDays-long trailing window when the
// caller passes a zero range, and rejects an explicit End before Start.
func resolveRange(rng domain.DateRange) (start, end time.Time, err error) {
	if rng.End.IsZero() {
		rng.End = time.Now().UTC()
	}
	if rng.Start.IsZero() {
		rng.Start = rng.End.AddDate(0, 0, -defaultRangeDays)
	}

	if rng.End.Before(rng.Start) {
		return time.Time{}, time.Time{}, fmt.Errorf("range end (%s) is before start (%s)", rng.End, rng.Start)
	}

	return truncateToDay(rng.Start), truncateToDay(rng.End), nil
}

func resolveCohortWeeks(weeks int32) int {
	if weeks <= 0 {
		return defaultCohortWeeks
	}

	return int(weeks)
}

func truncateToDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/analytics/internal/domain"
)

// maxRetentionWeekOffset caps how many weeks past a cohort's signup week
// this reader will ever compute retention for — an MVP safety bound so a
// very old cohort doesn't blow up the query with hundreds of offset rows.
const maxRetentionWeekOffset = 12

// Reader is the query-side repository backing the gRPC AnalyticsService —
// every method here is read-only and safe to call concurrently.
type Reader struct {
	pool *pgxpool.Pool
}

func NewReader(pool *pgxpool.Pool) *Reader {
	return &Reader{pool: pool}
}

// GetTripFunnel returns one row per day in [start, end] (inclusive) that
// has any funnel activity, plus the summed totals across the whole range.
func (r *Reader) GetTripFunnel(ctx context.Context, start, end time.Time) ([]domain.FunnelDayPoint, domain.FunnelDayPoint, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT day, requested_count, accepted_count, started_count, completed_count, cancelled_count
		FROM trip_funnel_daily
		WHERE day BETWEEN $1 AND $2
		ORDER BY day
	`, start, end)
	if err != nil {
		return nil, domain.FunnelDayPoint{}, fmt.Errorf("query trip funnel: %w", err)
	}
	defer rows.Close()

	var days []domain.FunnelDayPoint
	var totals domain.FunnelDayPoint

	for rows.Next() {
		var p domain.FunnelDayPoint
		if err := rows.Scan(&p.Day, &p.RequestedCount, &p.AcceptedCount, &p.StartedCount, &p.CompletedCount, &p.CancelledCount); err != nil {
			return nil, domain.FunnelDayPoint{}, fmt.Errorf("scan trip funnel row: %w", err)
		}

		days = append(days, p)
		totals.RequestedCount += p.RequestedCount
		totals.AcceptedCount += p.AcceptedCount
		totals.StartedCount += p.StartedCount
		totals.CompletedCount += p.CompletedCount
		totals.CancelledCount += p.CancelledCount
	}
	if err := rows.Err(); err != nil {
		return nil, domain.FunnelDayPoint{}, fmt.Errorf("iterate trip funnel rows: %w", err)
	}

	return days, totals, nil
}

// GetCancellationBreakdown groups cancellations in [start, end] by the trip
// stage they occurred at, and computes a rate against total requested trips
// in the same range (from trip_funnel_daily, the same source of truth the
// funnel endpoint uses).
func (r *Reader) GetCancellationBreakdown(ctx context.Context, start, end time.Time) (domain.CancellationBreakdown, error) {
	stageRows, err := r.pool.Query(ctx, `
		SELECT stage_at_cancellation, count(*)
		FROM trip_cancellations
		WHERE cancelled_at >= $1 AND cancelled_at < $2::timestamptz + INTERVAL '1 day'
		GROUP BY stage_at_cancellation
		ORDER BY stage_at_cancellation
	`, start, end)
	if err != nil {
		return domain.CancellationBreakdown{}, fmt.Errorf("query cancellation stages: %w", err)
	}
	defer stageRows.Close()

	var breakdown domain.CancellationBreakdown

	for stageRows.Next() {
		var stage string
		var count int64
		if err := stageRows.Scan(&stage, &count); err != nil {
			return domain.CancellationBreakdown{}, fmt.Errorf("scan cancellation stage row: %w", err)
		}

		breakdown.ByStage = append(breakdown.ByStage, domain.CancellationStageCount{
			Stage: domain.TripStage(stage),
			Count: count,
		})
		breakdown.TotalCancellations += count
	}
	if err := stageRows.Err(); err != nil {
		return domain.CancellationBreakdown{}, fmt.Errorf("iterate cancellation stage rows: %w", err)
	}

	var totalRequested int64
	err = r.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(requested_count), 0)
		FROM trip_funnel_daily
		WHERE day BETWEEN $1 AND $2
	`, start, end).Scan(&totalRequested)
	if err != nil {
		return domain.CancellationBreakdown{}, fmt.Errorf("query total requested trips: %w", err)
	}

	breakdown.TotalTrips = totalRequested

	if totalRequested > 0 {
		breakdown.CancellationRatePct = decimal.NewFromInt(breakdown.TotalCancellations).
			DivRound(decimal.NewFromInt(totalRequested), 4).
			Mul(decimal.NewFromInt(100))
	} else {
		breakdown.CancellationRatePct = decimal.Zero
	}

	return breakdown, nil
}

// GetRevenueSummary returns one row per (day, currency) in [start, end],
// plus totals. Note: if a deployment ever mixes currencies, the totals sum
// across them blindly (an MVP limitation — most deployments run a single
// currency, per pricing-service's own versioned per-deployment currency).
func (r *Reader) GetRevenueSummary(ctx context.Context, start, end time.Time) ([]domain.RevenueDayPoint, domain.RevenueSummary, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT day, currency, gross_fare_total, commission_total, trip_count
		FROM revenue_daily
		WHERE day BETWEEN $1 AND $2
		ORDER BY day, currency
	`, start, end)
	if err != nil {
		return nil, domain.RevenueSummary{}, fmt.Errorf("query revenue: %w", err)
	}
	defer rows.Close()

	var days []domain.RevenueDayPoint
	summary := domain.RevenueSummary{
		GrossFareTotal:  decimal.Zero,
		CommissionTotal: decimal.Zero,
	}

	for rows.Next() {
		var p domain.RevenueDayPoint
		if err := rows.Scan(&p.Day, &p.Currency, &p.GrossFareTotal, &p.CommissionTotal, &p.TripCount); err != nil {
			return nil, domain.RevenueSummary{}, fmt.Errorf("scan revenue row: %w", err)
		}

		days = append(days, p)
		summary.GrossFareTotal = summary.GrossFareTotal.Add(p.GrossFareTotal)
		summary.CommissionTotal = summary.CommissionTotal.Add(p.CommissionTotal)
		summary.TotalTrips += p.TripCount
	}
	if err := rows.Err(); err != nil {
		return nil, domain.RevenueSummary{}, fmt.Errorf("iterate revenue rows: %w", err)
	}

	summary.Days = days

	return days, summary, nil
}

// GetRiderRetention computes, for each rider signup cohort week in the last
// cohortWeeksBack weeks, what fraction of that cohort was active in each
// subsequent elapsed week.
func (r *Reader) GetRiderRetention(ctx context.Context, cohortWeeksBack int) ([]domain.RetentionCohort, error) {
	return r.getRetention(ctx, "rider_cohorts", "rider_weekly_activity", "rider_id", cohortWeeksBack)
}

// GetDriverRetention is the driver-side equivalent of GetRiderRetention.
func (r *Reader) GetDriverRetention(ctx context.Context, cohortWeeksBack int) ([]domain.RetentionCohort, error) {
	return r.getRetention(ctx, "driver_cohorts", "driver_weekly_activity", "driver_id", cohortWeeksBack)
}

// getRetention is unexported and only ever called with the four hardcoded
// literal table/column names above — never with caller-controlled input —
// so building them into the query string here is safe (same pattern as
// incrementFunnelColumn in writer.go).
func (r *Reader) getRetention(
	ctx context.Context,
	cohortsTable, activityTable, idColumn string,
	cohortWeeksBack int,
) ([]domain.RetentionCohort, error) {
	if cohortWeeksBack <= 0 {
		cohortWeeksBack = maxRetentionWeekOffset
	}
	if cohortWeeksBack > maxRetentionWeekOffset {
		cohortWeeksBack = maxRetentionWeekOffset
	}

	currentWeekStart := isoWeekStartOf(time.Now())
	earliestCohortWeek := currentWeekStart.AddDate(0, 0, -7*cohortWeeksBack)

	query := fmt.Sprintf(`
		WITH cohorts AS (
			SELECT cohort_week_start, count(*) AS cohort_size
			FROM %[1]s
			WHERE cohort_week_start >= $1
			GROUP BY cohort_week_start
		),
		offsets AS (
			SELECT generate_series(0, $2) AS week_offset
		),
		retention AS (
			SELECT
				c.cohort_week_start,
				o.week_offset,
				count(DISTINCT a.%[3]s) AS active_count
			FROM cohorts c
			CROSS JOIN offsets o
			JOIN %[1]s member ON member.cohort_week_start = c.cohort_week_start
			LEFT JOIN %[2]s a
				ON a.%[3]s = member.%[3]s
				AND a.activity_week_start = c.cohort_week_start + (o.week_offset * 7)
			WHERE c.cohort_week_start + (o.week_offset * 7) <= $3
			GROUP BY c.cohort_week_start, o.week_offset
		)
		SELECT c.cohort_week_start, c.cohort_size, r.week_offset, r.active_count
		FROM cohorts c
		JOIN retention r ON r.cohort_week_start = c.cohort_week_start
		ORDER BY c.cohort_week_start, r.week_offset
	`, cohortsTable, activityTable, idColumn)

	rows, err := r.pool.Query(ctx, query, earliestCohortWeek, maxRetentionWeekOffset, currentWeekStart)
	if err != nil {
		return nil, fmt.Errorf("query retention (%s): %w", cohortsTable, err)
	}
	defer rows.Close()

	cohortsByWeek := make(map[time.Time]*domain.RetentionCohort)
	var order []time.Time

	for rows.Next() {
		var cohortWeekStart time.Time
		var cohortSize, activeCount int64
		var weekOffset int

		if err := rows.Scan(&cohortWeekStart, &cohortSize, &weekOffset, &activeCount); err != nil {
			return nil, fmt.Errorf("scan retention row (%s): %w", cohortsTable, err)
		}

		cohort, exists := cohortsByWeek[cohortWeekStart]
		if !exists {
			cohort = &domain.RetentionCohort{
				CohortWeek: cohortWeekStart.Format("2006-01-02"),
				CohortSize: cohortSize,
			}
			cohortsByWeek[cohortWeekStart] = cohort
			order = append(order, cohortWeekStart)
		}

		var pct decimal.Decimal
		if cohortSize > 0 {
			pct = decimal.NewFromInt(activeCount).DivRound(decimal.NewFromInt(cohortSize), 4).Mul(decimal.NewFromInt(100))
		} else {
			pct = decimal.Zero
		}

		cohort.RetentionPercentByWeek = append(cohort.RetentionPercentByWeek, pct)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate retention rows (%s): %w", cohortsTable, err)
	}

	result := make([]domain.RetentionCohort, 0, len(order))
	for _, week := range order {
		result = append(result, *cohortsByWeek[week])
	}

	return result, nil
}

// isoWeekStartOf mirrors ingest.isoWeekStart (unexported in a different
// package, so duplicated here rather than introducing a cross-package
// dependency for one small function) — Monday 00:00 UTC of t's ISO week.
func isoWeekStartOf(t time.Time) time.Time {
	t = t.UTC()

	jan4 := time.Date(t.Year(), time.January, 4, 0, 0, 0, 0, time.UTC)

	isoWeekday := int(jan4.Weekday())
	if isoWeekday == 0 {
		isoWeekday = 7
	}

	week1Monday := jan4.AddDate(0, 0, -(isoWeekday - 1))

	_, week := t.ISOWeek()

	return week1Monday.AddDate(0, 0, (week-1)*7)
}

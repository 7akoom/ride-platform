package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/analytics/internal/domain"
)

// Reader answers the reports. Read-only, safe to call concurrently.
type Reader struct {
	pool *pgxpool.Pool
}

func NewReader(pool *pgxpool.Pool) *Reader {
	return &Reader{pool: pool}
}

// scopeSQL filters trip_facts by the scope given as $4, $5, $6.
const scopeSQL = `
	AND ($4 = '' OR city_id = $4)
	AND ($5 = '' OR zone_id = $5)
	AND ($6 = '' OR vehicle_class = $6)`

func windowArgs(w domain.Window, s domain.Scope) []any {
	return []any{w.Location.String(), w.Start, w.End, s.CityID, s.ZoneID, s.VehicleClass}
}

// TripFunnel returns every day of the window (zeros included) with the
// trips requested that day and how far they got.
func (r *Reader) TripFunnel(ctx context.Context, w domain.Window, s domain.Scope) ([]domain.FunnelDayPoint, domain.FunnelDayPoint, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT (requested_at AT TIME ZONE $1)::date AS day,
		       count(*), count(accepted_at), count(started_at), count(completed_at), count(cancelled_at)
		FROM trip_facts
		WHERE requested_at >= $2 AND requested_at < $3`+scopeSQL+`
		GROUP BY day`,
		windowArgs(w, s)...)
	if err != nil {
		return nil, domain.FunnelDayPoint{}, fmt.Errorf("query trip funnel: %w", err)
	}
	defer rows.Close()

	byDay := map[string]domain.FunnelDayPoint{}

	for rows.Next() {
		var p domain.FunnelDayPoint
		if err := rows.Scan(&p.Day, &p.RequestedCount, &p.AcceptedCount, &p.StartedCount, &p.CompletedCount, &p.CancelledCount); err != nil {
			return nil, domain.FunnelDayPoint{}, fmt.Errorf("scan trip funnel: %w", err)
		}

		byDay[p.Day.Format(time.DateOnly)] = p
	}

	if err := rows.Err(); err != nil {
		return nil, domain.FunnelDayPoint{}, fmt.Errorf("read trip funnel: %w", err)
	}

	days := make([]domain.FunnelDayPoint, 0, w.Days())

	var totals domain.FunnelDayPoint

	for day := w.From; !day.After(w.To); day = day.AddDate(0, 0, 1) {
		p := byDay[day.Format(time.DateOnly)]
		p.Day = day
		days = append(days, p)

		totals.RequestedCount += p.RequestedCount
		totals.AcceptedCount += p.AcceptedCount
		totals.StartedCount += p.StartedCount
		totals.CompletedCount += p.CompletedCount
		totals.CancelledCount += p.CancelledCount
	}

	return days, totals, nil
}

// Cancellations counts the cancelled trips among those requested in the
// window.
func (r *Reader) Cancellations(ctx context.Context, w domain.Window, s domain.Scope) (domain.CancellationBreakdown, error) {
	var out domain.CancellationBreakdown

	err := r.pool.QueryRow(ctx, `
		SELECT count(*), count(cancelled_at), count(*) FILTER (WHERE cancelled_at IS NOT NULL AND rider_no_show)
		FROM trip_facts
		WHERE requested_at >= $2 AND requested_at < $3 AND $1 <> ''`+scopeSQL,
		windowArgs(w, s)...).Scan(&out.TotalTrips, &out.TotalCancellations, &out.RiderNoShows)
	if err != nil {
		return out, fmt.Errorf("count cancellations: %w", err)
	}

	stages := map[domain.CancelStage]int64{}
	by := map[string]int64{}

	rows, err := r.pool.Query(ctx, `
		SELECT COALESCE(cancel_stage, 'requested'), COALESCE(cancelled_by, 'unknown'), count(*)
		FROM trip_facts
		WHERE cancelled_at IS NOT NULL AND requested_at >= $2 AND requested_at < $3 AND $1 <> ''`+scopeSQL+`
		GROUP BY 1, 2`,
		windowArgs(w, s)...)
	if err != nil {
		return out, fmt.Errorf("query cancellations: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var stage, cancelledBy string

		var count int64

		if err := rows.Scan(&stage, &cancelledBy, &count); err != nil {
			return out, fmt.Errorf("scan cancellations: %w", err)
		}

		stages[domain.CancelStage(stage)] += count
		by[cancelledBy] += count
	}

	if err := rows.Err(); err != nil {
		return out, fmt.Errorf("read cancellations: %w", err)
	}

	for _, stage := range domain.CancelStages {
		out.ByStage = append(out.ByStage, domain.CancellationStageCount{Stage: stage, Count: stages[stage]})
	}

	for _, who := range []string{"rider", "driver", "system", "unknown"} {
		if count, ok := by[who]; ok {
			out.ByCancelledBy = append(out.ByCancelledBy, domain.CancellationByCount{CancelledBy: who, Count: count})
		}
	}

	out.CancellationRatePct = percent(out.TotalCancellations, out.TotalTrips)

	return out, nil
}

// Revenue sums fares by the day they were worked out, per currency.
func (r *Reader) Revenue(ctx context.Context, w domain.Window, s domain.Scope) ([]domain.RevenueDayPoint, domain.RevenueSummary, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT (fare_at AT TIME ZONE $1)::date AS day, currency,
		       COALESCE(sum(fare_total) FILTER (WHERE fare_kind = 'trip'), 0),
		       count(*) FILTER (WHERE fare_kind = 'trip'),
		       COALESCE(sum(fare_total) FILTER (WHERE fare_kind <> 'trip'), 0),
		       count(*) FILTER (WHERE fare_kind <> 'trip'),
		       COALESCE(sum(commission), 0)
		FROM trip_facts
		WHERE fare_at >= $2 AND fare_at < $3 AND currency IS NOT NULL`+scopeSQL+`
		GROUP BY day, currency
		ORDER BY day, currency`,
		windowArgs(w, s)...)
	if err != nil {
		return nil, domain.RevenueSummary{}, fmt.Errorf("query revenue: %w", err)
	}
	defer rows.Close()

	var (
		days       []domain.RevenueDayPoint
		summary    domain.RevenueSummary
		currencies = map[string]struct{}{}
	)

	for rows.Next() {
		var p domain.RevenueDayPoint
		if err := rows.Scan(&p.Day, &p.Currency, &p.GrossFareTotal, &p.TripCount, &p.FeeTotal, &p.FeeCount, &p.CommissionTotal); err != nil {
			return nil, domain.RevenueSummary{}, fmt.Errorf("scan revenue: %w", err)
		}

		days = append(days, p)
		currencies[p.Currency] = struct{}{}

		summary.GrossFareTotal = summary.GrossFareTotal.Add(p.GrossFareTotal)
		summary.CommissionTotal = summary.CommissionTotal.Add(p.CommissionTotal)
		summary.FeeTotal = summary.FeeTotal.Add(p.FeeTotal)
		summary.TotalTrips += p.TripCount
		summary.TotalFees += p.FeeCount
	}

	if err := rows.Err(); err != nil {
		return nil, domain.RevenueSummary{}, fmt.Errorf("read revenue: %w", err)
	}

	// One instance has one currency; totals across several would mean nothing.
	if len(currencies) == 1 {
		for c := range currencies {
			summary.Currency = c
		}
	} else if len(currencies) > 1 {
		summary = domain.RevenueSummary{Currency: "MIXED"}
	}

	return days, summary, nil
}

// RiderRetention: riders by signup week, active = requested a trip.
func (r *Reader) RiderRetention(ctx context.Context, loc *time.Location, firstWeek, thisWeek time.Time) ([]domain.RetentionCohort, error) {
	return r.retention(ctx, loc, firstWeek, thisWeek,
		`SELECT rider_id, signed_up_at FROM rider_signups`,
		`SELECT rider_id, requested_at FROM trip_facts WHERE rider_id IS NOT NULL AND requested_at IS NOT NULL`)
}

// DriverRetention: drivers by approval (else signup) week, active =
// accepted a trip.
func (r *Reader) DriverRetention(ctx context.Context, loc *time.Location, firstWeek, thisWeek time.Time) ([]domain.RetentionCohort, error) {
	return r.retention(ctx, loc, firstWeek, thisWeek,
		`SELECT driver_id, COALESCE(approved_at, signed_up_at) FROM driver_signups WHERE COALESCE(approved_at, signed_up_at) IS NOT NULL`,
		`SELECT driver_id, accepted_at FROM trip_facts WHERE driver_id IS NOT NULL AND accepted_at IS NOT NULL`)
}

// retention takes two literal queries from this file (never caller input):
// (id, started) for the members and (id, at) for their activity. firstWeek
// and thisWeek are Mondays on loc's clock.
func (r *Reader) retention(ctx context.Context, loc *time.Location, firstWeek, thisWeek time.Time, membersSQL, activitySQL string) ([]domain.RetentionCohort, error) {
	start := time.Date(firstWeek.Year(), firstWeek.Month(), firstWeek.Day(), 0, 0, 0, 0, loc)

	rows, err := r.pool.Query(ctx, `
		WITH members AS (
			SELECT m.id, date_trunc('week', m.started AT TIME ZONE $1)::date AS cohort
			FROM (`+membersSQL+`) AS m(id, started)
			WHERE m.started >= $2
		),
		activity AS (
			SELECT DISTINCT a.id, date_trunc('week', a.at AT TIME ZONE $1)::date AS week
			FROM (`+activitySQL+`) AS a(id, at)
			WHERE a.at >= $2
		)
		SELECT m.cohort, NULL::int, count(*) FROM members m GROUP BY m.cohort
		UNION ALL
		SELECT m.cohort, (a.week - m.cohort) / 7, count(DISTINCT m.id)
		FROM members m JOIN activity a ON a.id = m.id AND a.week >= m.cohort
		GROUP BY m.cohort, (a.week - m.cohort) / 7`,
		loc.String(), start)
	if err != nil {
		return nil, fmt.Errorf("query retention: %w", err)
	}
	defer rows.Close()

	sizes := map[string]int64{}
	active := map[string]map[int]int64{}

	for rows.Next() {
		var (
			cohort time.Time
			offset *int
			count  int64
		)

		if err := rows.Scan(&cohort, &offset, &count); err != nil {
			return nil, fmt.Errorf("scan retention: %w", err)
		}

		key := cohort.Format(time.DateOnly)
		if offset == nil {
			sizes[key] = count

			continue
		}

		if active[key] == nil {
			active[key] = map[int]int64{}
		}

		active[key][*offset] = count
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read retention: %w", err)
	}

	var out []domain.RetentionCohort

	for week := firstWeek; !week.After(thisWeek); week = week.AddDate(0, 0, 7) {
		key := week.Format(time.DateOnly)

		size, ok := sizes[key]
		if !ok {
			continue
		}

		cohort := domain.RetentionCohort{CohortWeek: key, CohortSize: size}
		weeks := int(thisWeek.Sub(week).Hours()/24)/7 + 1

		for offset := 0; offset < weeks; offset++ {
			cohort.RetentionPercentByWeek = append(cohort.RetentionPercentByWeek, percent(active[key][offset], size))
		}

		out = append(out, cohort)
	}

	return out, nil
}

func percent(part, whole int64) decimal.Decimal {
	if whole == 0 {
		return decimal.Zero
	}

	return decimal.NewFromInt(part).Mul(decimal.NewFromInt(100)).DivRound(decimal.NewFromInt(whole), 2)
}

package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/7akoom/ride-platform/services/analytics/internal/domain"
)

// durationSQL is count, average, median and 90th percentile, in whole
// seconds, of an interval expression (a literal from this file).
func durationSQL(interval string) string {
	d := "(CASE WHEN " + interval + " IS NULL THEN NULL ELSE GREATEST(extract(epoch FROM " + interval + "), 0) END)"

	return fmt.Sprintf(`count(%[1]s), COALESCE(round(avg(%[1]s)), 0)::bigint,
		COALESCE(round(percentile_cont(0.5) WITHIN GROUP (ORDER BY %[1]s)), 0)::bigint,
		COALESCE(round(percentile_cont(0.9) WITHIN GROUP (ORDER BY %[1]s)), 0)::bigint`, d)
}

// ServiceLevels returns every day of the window and the whole range (zero
// Day) for trips requested then.
func (r *Reader) ServiceLevels(ctx context.Context, w domain.Window, s domain.Scope) ([]domain.ServiceLevelDay, domain.ServiceLevelDay, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT (requested_at AT TIME ZONE $1)::date AS day, count(*), count(completed_at),
		       `+durationSQL("accepted_at - requested_at")+`,
		       `+durationSQL("arrived_at - accepted_at")+`,
		       `+durationSQL("completed_at - started_at")+`
		FROM trip_facts
		WHERE requested_at >= $2 AND requested_at < $3`+scopeSQL+`
		GROUP BY GROUPING SETS ((day), ())`,
		windowArgs(w, s)...)
	if err != nil {
		return nil, domain.ServiceLevelDay{}, fmt.Errorf("query service levels: %w", err)
	}
	defer rows.Close()

	byDay := map[string]domain.ServiceLevelDay{}

	var totals domain.ServiceLevelDay

	for rows.Next() {
		var (
			day *time.Time
			p   domain.ServiceLevelDay
		)

		if err := rows.Scan(&day, &p.Requested, &p.Completed,
			&p.Match.Count, &p.Match.Average, &p.Match.Median, &p.Match.P90,
			&p.Pickup.Count, &p.Pickup.Average, &p.Pickup.Median, &p.Pickup.P90,
			&p.Ride.Count, &p.Ride.Average, &p.Ride.Median, &p.Ride.P90); err != nil {
			return nil, totals, fmt.Errorf("scan service levels: %w", err)
		}

		if day == nil {
			totals = p

			continue
		}

		p.Day = *day
		byDay[day.Format(time.DateOnly)] = p
	}

	if err := rows.Err(); err != nil {
		return nil, totals, fmt.Errorf("read service levels: %w", err)
	}

	days := make([]domain.ServiceLevelDay, 0, w.Days())
	for day := w.From; !day.After(w.To); day = day.AddDate(0, 0, 1) {
		p := byDay[day.Format(time.DateOnly)]
		p.Day = day
		days = append(days, p)
	}

	return days, totals, nil
}

// DriverOffers returns every day of the window with the offers sent then
// and how they ended; now decides which open ones ran out.
func (r *Reader) DriverOffers(ctx context.Context, w domain.Window, s domain.Scope, now time.Time) ([]domain.OfferDay, domain.OfferDay, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT (o.offered_at AT TIME ZONE $1)::date AS day, count(*),
		       count(*) FILTER (WHERE o.outcome = 'accepted'),
		       count(*) FILTER (WHERE o.outcome = 'rejected'),
		       count(*) FILTER (WHERE o.outcome IS NULL AND o.expires_at <= $7),
		       count(*) FILTER (WHERE o.outcome IS NULL AND o.expires_at > $7)
		FROM offer_facts o
		LEFT JOIN trip_facts t ON t.trip_id = o.trip_id
		WHERE o.offered_at >= $2 AND o.offered_at < $3`+scopeSQL+`
		GROUP BY day`,
		append(windowArgs(w, s), now)...)
	if err != nil {
		return nil, domain.OfferDay{}, fmt.Errorf("query offers: %w", err)
	}
	defer rows.Close()

	byDay := map[string]domain.OfferDay{}

	for rows.Next() {
		var p domain.OfferDay
		if err := rows.Scan(&p.Day, &p.Offered, &p.Accepted, &p.Rejected, &p.Expired, &p.Pending); err != nil {
			return nil, domain.OfferDay{}, fmt.Errorf("scan offers: %w", err)
		}

		byDay[p.Day.Format(time.DateOnly)] = p
	}

	if err := rows.Err(); err != nil {
		return nil, domain.OfferDay{}, fmt.Errorf("read offers: %w", err)
	}

	days := make([]domain.OfferDay, 0, w.Days())

	var totals domain.OfferDay

	for day := w.From; !day.After(w.To); day = day.AddDate(0, 0, 1) {
		p := byDay[day.Format(time.DateOnly)]
		p.Day = day
		days = append(days, p)

		totals.Offered += p.Offered
		totals.Accepted += p.Accepted
		totals.Rejected += p.Rejected
		totals.Expired += p.Expired
		totals.Pending += p.Pending
	}

	return days, totals, nil
}

// Ratings sums the stars on trips completed in the window.
func (r *Reader) Ratings(ctx context.Context, w domain.Window, s domain.Scope) (domain.Ratings, error) {
	var out domain.Ratings

	stars := func(column string) string {
		return fmt.Sprintf(`count(%[1]s), COALESCE(round(avg(%[1]s), 2), 0),
			count(*) FILTER (WHERE %[1]s = 1), count(*) FILTER (WHERE %[1]s = 2), count(*) FILTER (WHERE %[1]s = 3),
			count(*) FILTER (WHERE %[1]s = 4), count(*) FILTER (WHERE %[1]s = 5)`, column)
	}

	d, rd := &out.Drivers, &out.Riders

	err := r.pool.QueryRow(ctx, `
		SELECT count(*), `+stars("rider_stars")+`, `+stars("driver_stars")+`
		FROM trip_facts
		WHERE completed_at >= $1 AND completed_at < $2`+scopeFrom(3),
		rangeArgs(w, s)...).Scan(&out.CompletedTrips,
		&d.Count, &d.Average, &d.ByStars[0], &d.ByStars[1], &d.ByStars[2], &d.ByStars[3], &d.ByStars[4],
		&rd.Count, &rd.Average, &rd.ByStars[0], &rd.ByStars[1], &rd.ByStars[2], &rd.ByStars[3], &rd.ByStars[4])
	if err != nil {
		return out, fmt.Errorf("query ratings: %w", err)
	}

	return out, nil
}

// LiveTrips counts trips under way at now. Trips that never got an ending
// event stop counting after a few hours, so a lost event does not stay
// "live" forever.
func (r *Reader) LiveTrips(ctx context.Context, s domain.Scope, now time.Time) (domain.LiveTrips, error) {
	var out domain.LiveTrips

	err := r.pool.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE accepted_at IS NULL AND requested_at >= $1::timestamptz - interval '2 hours'),
			count(*) FILTER (WHERE accepted_at IS NOT NULL AND started_at IS NULL AND accepted_at >= $1::timestamptz - interval '6 hours'),
			count(*) FILTER (WHERE started_at IS NOT NULL AND started_at >= $1::timestamptz - interval '12 hours')
		FROM trip_facts
		WHERE cancelled_at IS NULL AND completed_at IS NULL
		  AND requested_at >= $1::timestamptz - interval '1 day'`+scopeFrom(2),
		now, s.CityID, s.ZoneID, s.VehicleClass).Scan(&out.Waiting, &out.OnTheWay, &out.InProgress)
	if err != nil {
		return out, fmt.Errorf("count live trips: %w", err)
	}

	return out, nil
}

package pricing

import (
	"context"
	"time"
)

// maxSurgePercent caps the combined surge so the three sources can't
// compound into an unreasonable multiplier (e.g. peak hour + few
// drivers + a storm all at once). 150% extra = 2.5x total, a ceiling
// that's easy to explain to a rider or support agent.
const maxSurgePercent = 150.0

// demandSearchRadiusMeters is how far around the pickup point we look
// for available drivers when judging scarcity.
const demandSearchRadiusMeters = 3000

func (s *service) calculateSurge(
	ctx context.Context,
	rules []SurgeTimeRule,
	pickupLat, pickupLng float64,
	now time.Time,
) SurgeBreakdown {
	timePercent := timeOfDaySurgePercent(rules, now)
	demandPercent := s.demandSurgePercent(ctx, pickupLat, pickupLng)
	weatherPercent := s.weatherSurgePercent(ctx, pickupLat, pickupLng)

	total := timePercent + demandPercent + weatherPercent
	if total > maxSurgePercent {
		total = maxSurgePercent
	}

	return SurgeBreakdown{
		TimeOfDayPercent: timePercent,
		DemandPercent:    demandPercent,
		WeatherPercent:   weatherPercent,
		TotalPercent:     total,
		Multiplier:       1 + total/100,
	}
}

// timeOfDaySurgePercent takes the HIGHEST matching active rule rather
// than summing overlapping rules — overlapping peak-hour windows are a
// configuration mistake, not a reason to double-charge riders.
func timeOfDaySurgePercent(rules []SurgeTimeRule, now time.Time) float64 {
	currentDay := int(now.Weekday()) // time.Sunday == 0, matches Postgres DOW
	currentTime := now.Format("15:04:05")

	highest := 0.0

	for _, rule := range rules {
		if rule.DayOfWeek != nil && *rule.DayOfWeek != currentDay {
			continue
		}

		if !timeInWindow(currentTime, rule.StartTime, rule.EndTime) {
			continue
		}

		if rule.SurgePercent > highest {
			highest = rule.SurgePercent
		}
	}

	return highest
}

// timeInWindow handles the ordinary case (start < end) and the
// overnight case (e.g. 22:00-02:00, where end < start) with plain
// string comparison since "HH:MM:SS" sorts correctly lexicographically.
func timeInWindow(current, start, end string) bool {
	if start <= end {
		return current >= start && current <= end
	}

	return current >= start || current <= end
}

// demandSurgePercent uses driver scarcity near the pickup point as a
// proxy for supply/demand imbalance. This is a deliberate v1
// simplification: the "correct" signal would also weigh how many
// riders are currently requesting trips in the area, but that needs a
// new endpoint on trip-service that doesn't exist yet (see this
// package's README). Fewer nearby available drivers -> higher percent.
func (s *service) demandSurgePercent(ctx context.Context, pickupLat, pickupLng float64) float64 {
	count, err := s.locationClient.CountNearbyAvailableDrivers(
		ctx,
		pickupLat,
		pickupLng,
		demandSearchRadiusMeters,
	)
	if err != nil {
		// Fail open: an unreachable location-service shouldn't block
		// fare calculation, it should just mean no demand surge applies.
		return 0
	}

	switch {
	case count == 0:
		return 100
	case count <= 2:
		return 50
	case count <= 5:
		return 20
	default:
		return 0
	}
}

func (s *service) weatherSurgePercent(ctx context.Context, pickupLat, pickupLng float64) float64 {
	conditions, err := s.weatherClient.GetConditions(ctx, pickupLat, pickupLng)
	if err != nil {
		// Fail open — see WeatherClient's port comment.
		return 0
	}

	return conditions.SurgePercent
}

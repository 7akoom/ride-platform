package pricing

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

// demandWindow is how far back quotes count as demand: riders who asked
// for a price in the zone in the last ten minutes are the ones looking for
// a ride now.
const demandWindow = 10 * time.Minute

var hundred = decimal.NewFromInt(100)

// surgeFor combines the market's surge sources under one rate card:
//
//	staff  = the larger of the hour's rule and the zone's surge
//	total  = staff + demand + weather, capped at the card's maximum
//
// The two staff sources never add up: a zone surge for a concert during
// rush hour is staff saying "this much", not "this much more". Demand and
// weather only count when the card lets them.
func surgeFor(config Config, m market) SurgeBreakdown {
	demand := decimal.Zero
	if config.DemandSurge {
		demand = demandSurgePercent(m)
	}

	weather := decimal.Zero
	if config.WeatherSurge {
		weather = m.weatherPercent
	}

	staff := m.timePercent
	if m.zonePercent.GreaterThan(staff) {
		staff = m.zonePercent
	}

	total := staff.Add(demand).Add(weather)

	ceiling := config.MaxSurgePercent
	if ceiling.IsNegative() {
		ceiling = decimal.Zero
	}

	if total.GreaterThan(ceiling) {
		total = ceiling
	}

	label := ""
	if staff.IsPositive() && total.IsPositive() {
		label = m.staffLabel
	}

	return SurgeBreakdown{
		TimeOfDayPercent: m.timePercent,
		ZonePercent:      m.zonePercent,
		DemandPercent:    demand,
		WeatherPercent:   weather,
		TotalPercent:     total,
		Multiplier:       decimal.NewFromInt(1).Add(total.Div(hundred)),
		Label:            label,
	}
}

// demandSurgePercent weighs the riders asking for a price in the zone
// against the free drivers near the pickup. Nothing is added when the
// drivers could not be looked up (fail open: an unreachable driver-service
// must not raise prices).
//
//	no free driver nearby          +50%
//	3 or more riders per driver    +50%
//	2 or more                      +30%
//	1.5 or more                    +15%
//	fewer                           0
func demandSurgePercent(m market) decimal.Decimal {
	if !m.driversKnown {
		return decimal.Zero
	}

	supply := len(m.drivers)
	if supply == 0 {
		return decimal.NewFromInt(50)
	}

	// The rider asking counts too.
	demand := float64(m.otherRiders + 1)
	ratio := demand / float64(supply)

	switch {
	case ratio >= 3:
		return decimal.NewFromInt(50)
	case ratio >= 2:
		return decimal.NewFromInt(30)
	case ratio >= 1.5:
		return decimal.NewFromInt(15)
	default:
		return decimal.Zero
	}
}

// rulesFor keeps the rules that apply to a pickup in the zone: the ones for
// everywhere, for its city and for the zone itself.
func rulesFor(rules []SurgeTimeRule, zone ServiceZone) []SurgeTimeRule {
	var out []SurgeTimeRule

	for _, rule := range rules {
		switch {
		case rule.ZoneID == "" && rule.CityID == "":
		case rule.ZoneID != "" && rule.ZoneID == zone.ZoneID:
		case rule.CityID != "" && rule.CityID == zone.CityID:
		default:
			continue
		}

		out = append(out, rule)
	}

	return out
}

// timeRuleSurge is the highest matching rule's percent and label at the
// given local time.
func timeRuleSurge(rules []SurgeTimeRule, localNow time.Time) (decimal.Decimal, string) {
	rule, found := highestMatchingRule(rules, localNow)
	if !found {
		return decimal.Zero, ""
	}

	return rule.SurgePercent, rule.Label
}

// timeOfDaySurgePercent takes the HIGHEST matching active rule rather
// than summing overlapping rules — overlapping peak-hour windows are a
// configuration mistake, not a reason to double-charge riders.
func timeOfDaySurgePercent(rules []SurgeTimeRule, now time.Time) decimal.Decimal {
	percent, _ := timeRuleSurge(rules, now)

	return percent
}

func highestMatchingRule(rules []SurgeTimeRule, now time.Time) (SurgeTimeRule, bool) {
	currentDay := int(now.Weekday()) // time.Sunday == 0, matches Postgres DOW
	currentTime := now.Format("15:04:05")

	var (
		best  SurgeTimeRule
		found bool
	)

	for _, rule := range rules {
		if !rule.Active {
			continue
		}

		if rule.DayOfWeek != nil && *rule.DayOfWeek != currentDay {
			continue
		}

		if !timeInWindow(currentTime, rule.StartTime, rule.EndTime) {
			continue
		}

		if !found || rule.SurgePercent.GreaterThan(best.SurgePercent) {
			best, found = rule, true
		}
	}

	return best, found && best.SurgePercent.IsPositive()
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

func (s *service) weatherSurgePercent(ctx context.Context, pickupLat, pickupLng float64) decimal.Decimal {
	conditions, err := s.weatherClient.GetConditions(ctx, pickupLat, pickupLng)
	if err != nil {
		// Fail open — see WeatherClient's port comment.
		return decimal.Zero
	}

	return conditions.SurgePercent
}

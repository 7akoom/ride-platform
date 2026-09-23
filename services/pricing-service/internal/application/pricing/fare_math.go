package pricing

import (
	"math"

	"github.com/shopspring/decimal"
)

const earthRadiusKm = 6371.0

// Route is the distance and duration between two points. When OSRM is
// reachable these are real road-network values; when it isn't, they're
// the Haversine fallback below (flagged via Estimated so callers and
// logs can tell the difference). Kept as float64 — a physical
// measurement, not money.
type Route struct {
	DistanceKm      float64
	DurationMinutes float64
	Estimated       bool
}

// haversineDistanceKm is the straight-line ("as the crow flies")
// distance between two points. Only used as a fallback when the routing
// engine is unreachable — see routeOrFallback.
func haversineDistanceKm(lat1, lng1, lat2, lng2 float64) float64 {
	lat1Rad := lat1 * math.Pi / 180
	lat2Rad := lat2 * math.Pi / 180
	deltaLat := (lat2 - lat1) * math.Pi / 180
	deltaLng := (lng2 - lng1) * math.Pi / 180

	a := math.Sin(deltaLat/2)*math.Sin(deltaLat/2) +
		math.Cos(lat1Rad)*math.Cos(lat2Rad)*
			math.Sin(deltaLng/2)*math.Sin(deltaLng/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return earthRadiusKm * c
}

// fallbackRoute approximates a road route without a routing engine:
// straight-line distance inflated by a correction factor, with duration
// derived from an assumed average speed. Deliberately conservative —
// it's better to be roughly right than to refuse to quote a price.
func fallbackRoute(config Config, pickupLat, pickupLng, dropoffLat, dropoffLng float64) Route {
	straightLineKm := haversineDistanceKm(pickupLat, pickupLng, dropoffLat, dropoffLng)
	distanceKm := straightLineKm * config.DistanceCorrectionFactor

	return Route{
		DistanceKm:      distanceKm,
		DurationMinutes: (distanceKm / config.AverageSpeedKmh) * 60,
		Estimated:       true,
	}
}

// baseFareBreakdown turns a route into the pre-surge, pre-discount
// subtotal using the rate card, raised to the card's minimum fare. Surge
// and discounts are applied on top of this by the caller. Route's
// distance/duration are float64 (physical measurements); they're converted
// to decimal only at the point they're multiplied against a decimal rate,
// exactly like wallet-service converts a wire decimal string at its own
// boundary.
func baseFareBreakdown(config Config, route Route) FareBreakdown {
	distanceFare := decimal.NewFromFloat(route.DistanceKm).Mul(config.PerKmRate)
	durationFare := decimal.NewFromFloat(route.DurationMinutes).Mul(config.PerMinuteRate)
	subtotal := config.BaseFare.Add(distanceFare).Add(durationFare)

	adjustment := decimal.Zero
	if config.MinimumFare.GreaterThan(subtotal) {
		adjustment = config.MinimumFare.Sub(subtotal)
		subtotal = config.MinimumFare
	}

	return FareBreakdown{
		CurrencyCode:          config.CurrencyCode,
		BaseFare:              config.BaseFare,
		DistanceKm:            route.DistanceKm,
		DistanceFare:          distanceFare,
		DurationMinutes:       route.DurationMinutes,
		DurationFare:          durationFare,
		MinimumFareAdjustment: adjustment,
		Subtotal:              subtotal,
	}
}

package trip

import (
	"context"
	"math"
	"strings"
)

// quoteMatchMeters is how far the trip's pickup and dropoff may be from the
// quoted ones: the same point picked twice on a map, not another trip.
const quoteMatchMeters = 50

// ClaimedQuote is what a claimed fare quote fixes for the trip.
type ClaimedQuote struct {
	ID           string
	VehicleClass string
	Pickup       Coordinates
	Dropoff      Coordinates
	// Total is a decimal string in CurrencyCode.
	Total        string
	CurrencyCode string
}

// QuoteBook claims fare quotes (pricing-service). Claim returns
// ErrQuoteNotFound for an unknown quote or another rider's, and
// ErrQuoteNotUsable for one that expired, was used, or whose coupon ended.
type QuoteBook interface {
	Claim(ctx context.Context, quoteID, riderID, tripID string) (ClaimedQuote, error)
	Release(ctx context.Context, quoteID, tripID string) error
}

// Option customises the trip service at construction time.
type Option func(*service)

// WithQuotes lets a trip be requested with a fare quote: the trip pays the
// quoted price. Without it a named quote is refused.
func WithQuotes(book QuoteBook) Option {
	if book == nil {
		panic("quote book is required")
	}

	return func(s *service) {
		s.quotes = book
	}
}

// matchesQuote reports whether the trip is the quoted one: the same pickup
// and dropoff (within quoteMatchMeters) and, when the rider named a class,
// the quoted class.
func matchesQuote(quote ClaimedQuote, pickup, dropoff Coordinates, requestedClass string) bool {
	if class := strings.TrimSpace(requestedClass); class != "" {
		normalized, err := NormalizeVehicleClass(class)
		if err != nil || normalized != quote.VehicleClass {
			return false
		}
	}

	return distanceMeters(pickup, quote.Pickup) <= quoteMatchMeters &&
		distanceMeters(dropoff, quote.Dropoff) <= quoteMatchMeters
}

// distanceMeters is the great-circle distance between two points.
func distanceMeters(a, b Coordinates) float64 {
	const earthRadiusMeters = 6_371_000

	lat1, lat2 := a.Latitude*math.Pi/180, b.Latitude*math.Pi/180
	dLat := lat2 - lat1
	dLng := (b.Longitude - a.Longitude) * math.Pi / 180

	h := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1)*math.Cos(lat2)*math.Sin(dLng/2)*math.Sin(dLng/2)

	return 2 * earthRadiusMeters * math.Asin(math.Min(1, math.Sqrt(h)))
}
